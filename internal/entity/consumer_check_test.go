package entity

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// 本文件按分层设计 §六 逐个走查 entity 的下游消费方，把「下游会怎么用」变成可执行
// 断言。目的不是覆盖率（零方法的包没有语句可覆盖），而是：本包的字段形状一旦发生
// 对下游有害的变化，在本包的测试里就当场失败，而不是等到下游那一轮才发现。
//
// 与 currency / enum 的同名文件同一取向。走查的消费方（分层 §七 依赖图中依赖 L1.1
// 的全部层）：
//
//	rule/convert(L2)      不变式 1a 折算三字段联动
//	rule/amortize(L2)     不变式 1b 起止月派生 + 8.2 均分
//	rule/dedup(L2)        8.3 四要素判重
//	store(L3/L4)          落库往返、归属签名、软删除、区间穿刺
//	service/fx(L5.2)      I-7 未收敛行识别
//	service/audit(L5.0)   不变式 4 先审计后挂载
//	service/intake(L5.4)  D-7 提交 / F-8 复制字段继承
//	api/http/dto(L6.0)    前提 6 与密码红线

// ---------- L2 规则层 ----------

// TestConsumerConvertInvariant1a 走查 rule/convert（L2）。
//
// 分层 §九 不变式 1a：本位币金额 = 支出金额 × 折算汇率，按**本位币的
// currency.小数位数**四舍五入；折算汇率先规整到 8 位（T39）。
//
// 要求本包提供的是：Amount / Rate / BaseAmount 三者可直接参与 decimal 运算，
// BaseCurrency 可直接取 Scale() 作舍入基准。
func TestConsumerConvertInvariant1a(t *testing.T) {
	// 模拟 rule/convert 的唯一实现：规整汇率 → 乘 → 按本位币位数舍入
	convert := func(e Expense, base currency.Code) (Expense, error) {
		if base.IsZero() {
			// currency.Code.Scale() 的零值返回 0 与 JPY 的 0 无法区分，必须先判
			return e, fmt.Errorf("本位币未指定")
		}
		e.Rate = e.Rate.RoundRate() // T39：8 位，全仓库唯一规整点
		amount, err := e.Amount.Mul(e.Rate).Round(base.Scale(), decimal.RoundHalfUp)
		if err != nil {
			return e, err
		}
		e.BaseAmount = amount
		e.BaseCurrency = base // 汇率重拉或手填 → 同步置为当前本位币（8.1）
		return e, nil
	}

	t.Run("CNY（2 位）折算后满足恒等式", func(t *testing.T) {
		e := Expense{
			Amount:   decimal.MustParse("100.00"),
			Currency: currency.MustParse("USD"),
			Rate:     decimal.MustParse("7.12345678"),
		}
		got, err := convert(e, currency.MustParse("CNY"))
		if err != nil {
			t.Fatalf("折算异常: %v", err)
		}
		if want := decimal.MustParse("712.35"); !got.BaseAmount.Equal(want) {
			t.Errorf("本位币金额: got %s, want %s", got.BaseAmount.String(), want.String())
		}
		if !got.BaseCurrency.Equal(currency.MustParse("CNY")) {
			t.Error("折算本位币币种应被置为当前本位币（8.1）")
		}
	})

	t.Run("JPY（0 位）按本位币位数舍入而非固定 2 位", func(t *testing.T) {
		// 这是分层 §三 问题 1 的修法：舍入基准是本位币小数位数，不是存储的 2 位。
		// 若按 2 位舍入，会算出 712.35 JPY——该币种不存在的面额。
		e := Expense{
			Amount:   decimal.MustParse("100.00"),
			Currency: currency.MustParse("USD"),
			Rate:     decimal.MustParse("7.12345678"),
		}
		got, err := convert(e, currency.MustParse("JPY"))
		if err != nil {
			t.Fatalf("折算异常: %v", err)
		}
		if want := decimal.MustParse("712"); !got.BaseAmount.Equal(want) {
			t.Errorf("JPY 本位币金额: got %s, want %s", got.BaseAmount.String(), want.String())
		}
		// T38 保证本位币位数 ≤ 2，故结果必然能被 2 位定点列无损容纳
		if !got.BaseAmount.FitsScale(decimal.BaseAmountStoreScale) {
			t.Error("按本位币位数算出的金额应能被 2 位定点列无损容纳（T38）")
		}
	})

	t.Run("负金额（退款）对称", func(t *testing.T) {
		e := Expense{
			Amount:   decimal.MustParse("-100.00"),
			Currency: currency.MustParse("USD"),
			Rate:     decimal.MustParse("7.00000000"),
		}
		got, err := convert(e, currency.MustParse("CNY"))
		if err != nil {
			t.Fatalf("折算异常: %v", err)
		}
		if !got.BaseAmount.IsNegative() {
			t.Error("负金额折算后应仍为负（终版 §四 3：允许为负，作退款/冲正）")
		}
	})
}

// TestConsumerConvertOnlyAmountChanged 走查终版 8.1 的第 1 行触发。
//
// 「改支出金额（汇率不动）」是六种触发中**唯一不改 折算本位币币种** 的一种。
// 本包必须让这一行为可表达：三个字段各自独立可写，不存在「改一个必然带改另一个」
// 的耦合（那样就得在本包实现联动，与零方法冲突）。
func TestConsumerConvertOnlyAmountChanged(t *testing.T) {
	// 一笔尚未收敛的行：折算本位币币种是 USD，当前本位币是 CNY
	e := Expense{
		Amount:       decimal.MustParse("100.00"),
		Currency:     currency.MustParse("USD"),
		Rate:         decimal.MustParse("7.00000000"),
		BaseAmount:   decimal.MustParse("700.00"),
		BaseCurrency: currency.MustParse("USD"),
	}
	before := e.BaseCurrency

	// 只改金额、汇率不动：本位币金额重算，折算本位币币种**保持原口径**
	e.Amount = decimal.MustParse("200.00")
	recalc, err := e.Amount.Mul(e.Rate).Round(currency.MustParse("USD").Scale(), decimal.RoundHalfUp)
	if err != nil {
		t.Fatalf("重算异常: %v", err)
	}
	e.BaseAmount = recalc

	if !e.BaseCurrency.Equal(before) {
		t.Error("只改金额时 折算本位币币种 必须保持原口径，否则未收敛行不再可被识别（8.1 第 1 行）")
	}
	if want := decimal.MustParse("1400.00"); !e.BaseAmount.Equal(want) {
		t.Errorf("重算后本位币金额: got %s, want %s", e.BaseAmount.String(), want.String())
	}
}

// TestConsumerAmortizeInvariant1b 走查 rule/amortize（L2）。
//
// 分层 §九 不变式 1b：起始月 = 支出日期所属月；结束月 = 起始月 + 月数 − 1。
// 要求 SpendDate 可直接 ToMonth()，AmortizeMonths 可直接参与月区间运算。
func TestConsumerAmortizeInvariant1b(t *testing.T) {
	derive := func(e Expense) (Expense, error) {
		if e.SpendDate.IsZero() {
			return e, fmt.Errorf("支出日期未指定")
		}
		r, err := calendar.NewMonthRange(e.SpendDate.ToMonth(), e.AmortizeMonths)
		if err != nil {
			return e, err
		}
		e.AmortizeStart = r.Start()
		e.AmortizeEnd = r.End()
		return e, nil
	}

	t.Run("默认 1 个月（不摊分）起止月相同", func(t *testing.T) {
		e := Expense{SpendDate: calendar.MustParseDate("2026-01-15"), AmortizeMonths: 1}
		got, err := derive(e)
		if err != nil {
			t.Fatalf("派生异常: %v", err)
		}
		if got.AmortizeStart.String() != "2026-01" || got.AmortizeEnd.String() != "2026-01" {
			t.Errorf("1 个月的起止月应同为 2026-01: got %s ~ %s",
				got.AmortizeStart.String(), got.AmortizeEnd.String())
		}
	})

	t.Run("跨年摊分 12 个月", func(t *testing.T) {
		e := Expense{SpendDate: calendar.MustParseDate("2026-11-30"), AmortizeMonths: 12}
		got, err := derive(e)
		if err != nil {
			t.Fatalf("派生异常: %v", err)
		}
		if got.AmortizeStart.String() != "2026-11" || got.AmortizeEnd.String() != "2027-10" {
			t.Errorf("起止月: got %s ~ %s, want 2026-11 ~ 2027-10",
				got.AmortizeStart.String(), got.AmortizeEnd.String())
		}
	})

	t.Run("摊分月数无上限（前提 5）", func(t *testing.T) {
		e := Expense{SpendDate: calendar.MustParseDate("2026-01-01"), AmortizeMonths: 600}
		got, err := derive(e)
		if err != nil {
			t.Fatalf("摊分月数无上限，600 个月应可派生: %v", err)
		}
		if got.AmortizeEnd.String() != "2075-12" {
			t.Errorf("600 个月的结束月: got %s, want 2075-12", got.AmortizeEnd.String())
		}
	})
}

// TestConsumerAmortizeSplit 走查 8.2 的均分与尾差。
//
// 要求：原币按 Currency 的小数位数均分、本位币按 BaseCurrency 的小数位数均分，
// 二者**各自独立**——这正是本包把 Currency 与 BaseCurrency 分成两个字段的原因。
func TestConsumerAmortizeSplit(t *testing.T) {
	// 模拟 rule/amortize 的均分：按币种小数位截断取每月份额，尾差归末月
	split := func(total decimal.Decimal, months int, c currency.Code) ([]decimal.Decimal, error) {
		quotient, remainder, err := total.QuoRem(decimal.FromInt(int64(months)), c.Scale())
		if err != nil {
			return nil, err
		}
		shares := make([]decimal.Decimal, months)
		for i := range shares {
			shares[i] = quotient
		}
		shares[months-1] = shares[months-1].Add(remainder) // 尾差固定计入末月
		return shares, nil
	}

	e := Expense{
		Amount:         decimal.MustParse("100.00"),
		Currency:       currency.MustParse("CNY"), // 2 位
		BaseAmount:     decimal.MustParse("1400"),
		BaseCurrency:   currency.MustParse("JPY"), // 0 位
		AmortizeMonths: 3,
	}

	t.Run("原币按支出币种位数均分且合计等于原额", func(t *testing.T) {
		shares, err := split(e.Amount, e.AmortizeMonths, e.Currency)
		if err != nil {
			t.Fatalf("均分异常: %v", err)
		}
		if !decimal.Sum(shares...).Equal(e.Amount) {
			t.Errorf("份额合计 %s ≠ 原额 %s", decimal.Sum(shares...).String(), e.Amount.String())
		}
		// 100.00 / 3 按 2 位截断 = 33.33，尾差 0.01 归末月
		if shares[0].String() != "33.33" || shares[2].String() != "33.34" {
			t.Errorf("份额: got %s/%s/%s, want 33.33/33.33/33.34",
				shares[0].String(), shares[1].String(), shares[2].String())
		}
	})

	t.Run("本位币按本位币位数独立均分", func(t *testing.T) {
		shares, err := split(e.BaseAmount, e.AmortizeMonths, e.BaseCurrency)
		if err != nil {
			t.Fatalf("均分异常: %v", err)
		}
		if !decimal.Sum(shares...).Equal(e.BaseAmount) {
			t.Error("本位币份额合计应等于本位币金额")
		}
		// JPY 0 位：1400 / 3 = 466，尾差 2 归末月
		if shares[0].String() != "466" || shares[2].String() != "468" {
			t.Errorf("JPY 份额: got %s/%s/%s, want 466/466/468",
				shares[0].String(), shares[1].String(), shares[2].String())
		}
	})
}

// TestConsumerDedupFourElements 走查 rule/dedup（L2）。
//
// 终版 8.3：四要素（归属用户 + 支出日期 + 支出金额 + 支出币种）**等值匹配**，
// 不设指纹属性，不纳入卡片、对手方、备注等非必有字段。
//
// 要求本包提供的是：四个字段各自可比较，且**金额必须走 Equal 而非 ==**——
// decimal.Decimal 刻意做成不可比较，正是为了让 == 变成编译错误。
func TestConsumerDedupFourElements(t *testing.T) {
	// 模拟 rule/dedup 的四要素等值判定
	sameKey := func(a, b Expense) bool {
		return a.OwnerUserID == b.OwnerUserID &&
			a.SpendDate.Equal(b.SpendDate) &&
			a.Amount.Equal(b.Amount) && // 必须 Equal：1.50 与 1.5 数值相等
			a.Currency.Equal(b.Currency)
	}

	base := Expense{
		OwnerUserID: 260907000000000001,
		SpendDate:   calendar.MustParseDate("2026-01-15"),
		Amount:      decimal.MustParse("100.50"),
		Currency:    currency.MustParse("CNY"),
	}

	t.Run("四要素相同即疑似重复", func(t *testing.T) {
		other := base
		other.ID = 260907000000000002
		other.Counterparty = "另一个商户" // 非四要素，不影响判定
		other.Remark = "另一个备注"
		other.CardLast4 = "9999"
		if !sameKey(base, other) {
			t.Error("四要素相同应判为疑似重复，非必有字段不参与判定（8.3）")
		}
	})

	t.Run("金额表示不同但数值相同仍算重复", func(t *testing.T) {
		// 这是 decimal 做成不可比较的直接理由：== 在此会得 false，静默漏判
		other := base
		other.Amount = decimal.MustParse("100.5") // 与 100.50 数值相等
		if !sameKey(base, other) {
			t.Error("100.5 与 100.50 数值相等，应判为重复；此处若失败说明用了 == 而非 Equal")
		}
	})

	t.Run("归属用户不同则不重复", func(t *testing.T) {
		other := base
		other.OwnerUserID = 260907000000000009
		if sameKey(base, other) {
			t.Error("四要素含归属用户，跨用户不应判重（前提 3 纯个人数据隔离）")
		}
	})

	t.Run("币种不同则不重复", func(t *testing.T) {
		other := base
		other.Currency = currency.MustParse("USD")
		if sameKey(base, other) {
			t.Error("四要素含支出币种，币种不同不应判重")
		}
	})
}

// ---------- L3 存储层 ----------

// TestConsumerStorePersistence 走查 store / store/sqlite（L3/L4）。
//
// 准则 3「枚举即代码，库中只存代码 / 键」+ 分层 §六 L3 store ⑥「金额 2 位定点、
// 汇率 8 位定点，一律非浮点」。要求 62 个字段全部有确定的落库形态，且能无损读回。
func TestConsumerStorePersistence(t *testing.T) {
	t.Run("枚举与日期字段实现 Valuer/Scanner", func(t *testing.T) {
		// 落库形态由字段类型自带，store 无需为每个字段写转换
		e := Expense{
			SpendDate:     calendar.MustParseDate("2026-01-15"),
			Currency:      currency.MustParse("CNY"),
			CardCurrency:  currency.Code{}, // 零值：解析不出则留空
			AmortizeStart: calendar.MustParseMonth("2026-01"),
		}
		if v, err := e.SpendDate.Value(); err != nil || v != "2026-01-15" {
			t.Errorf("支出日期落库形态: got %v, err %v, want 定长文本", v, err)
		}
		if v, err := e.Currency.Value(); err != nil || v != "CNY" {
			t.Errorf("支出币种落库形态: got %v, err %v, want 代码文本", v, err)
		}
		// 可空字段的零值落 NULL，让必填列被 NOT NULL 当场拦下
		if v, err := e.CardCurrency.Value(); err != nil || v != nil {
			t.Errorf("卡片币种零值应落 NULL: got %v, err %v", v, err)
		}
		if v, err := e.AmortizeStart.Value(); err != nil || v != "2026-01" {
			t.Errorf("摊分起始月落库形态: got %v, err %v", v, err)
		}
	})

	t.Run("金额按 2 位、汇率按 8 位落定点整数", func(t *testing.T) {
		e := Expense{
			Amount:     decimal.MustParse("100.50"),
			BaseAmount: decimal.MustParse("712.35"),
			Rate:       decimal.MustParse("7.12345678"),
		}
		// store 显式指定位数（decimal 刻意不实现 Valuer，避免替 store 猜位数）
		if u, err := e.Amount.Units(2); err != nil || u != 10050 {
			t.Errorf("支出金额最小单位: got %d, err %v, want 10050", u, err)
		}
		if u, err := e.BaseAmount.Units(decimal.BaseAmountStoreScale); err != nil || u != 71235 {
			t.Errorf("本位币金额最小单位: got %d, err %v, want 71235", u, err)
		}
		if u, err := e.Rate.Units(decimal.RateScale); err != nil || u != 712345678 {
			t.Errorf("折算汇率最小单位: got %d, err %v, want 712345678", u, err)
		}
	})

	t.Run("往返无损", func(t *testing.T) {
		// 模拟 store/sqlite 的一次「写 → 读」往返
		src := Expense{
			SpendDate: calendar.MustParseDate("2028-02-29"), // 闰年 2 月 29 日
			Amount:    decimal.MustParse("-0.01"),           // 负金额（退款）
			Currency:  currency.MustParse("KWD"),            // 3 位币种作支出币种（T38 允许）
			Rate:      decimal.MustParse("23.12345678"),
		}
		var dst Expense
		dateVal, _ := src.SpendDate.Value()
		if err := dst.SpendDate.Scan(dateVal); err != nil {
			t.Fatalf("日期读回异常: %v", err)
		}
		curVal, _ := src.Currency.Value()
		if err := dst.Currency.Scan(curVal); err != nil {
			t.Fatalf("币种读回异常: %v", err)
		}
		amountUnits, _ := src.Amount.Units(src.Currency.Scale())
		readBack, err := decimal.FromUnits(amountUnits, src.Currency.Scale())
		if err != nil {
			t.Fatalf("金额读回异常: %v", err)
		}
		dst.Amount = readBack

		if !dst.SpendDate.Equal(src.SpendDate) {
			t.Error("支出日期往返后不等")
		}
		if !dst.Currency.Equal(src.Currency) {
			t.Error("支出币种往返后不等")
		}
		if !dst.Amount.Equal(src.Amount) {
			t.Errorf("支出金额往返后不等: %s vs %s", dst.Amount.String(), src.Amount.String())
		}
	})
}

// TestConsumerStoreOwnership 走查不变式 3：4 个归属型实体全部带归属用户字段。
//
// 分层 §九 不变式 3：「5 个实体全部带归属用户，任何按 ID 的访问都必须校验归属——
// 漏一处等于全量泄露」。其中 User 自身即归属主体（其「归属」就是 ID），
// 另外 4 个必须各有一个指回 User.ID 的字段。
//
// 这条不能只靠 golden 表：那里锁的是字段名不变，这里锁的是**语义上必须存在**
// ——将来新增第 6 个实体时，此处的表会提醒补上归属字段。
func TestConsumerStoreOwnership(t *testing.T) {
	// AuditLog 的归属字段名是 UserID 而非 OwnerUserID，理由见该字段注释
	// （登录失败记录中它表示被尝试账号的归属，不代表操作人身份）
	cases := []struct {
		entity string
		target any
		field  string
	}{
		{"ExpenseCategory", ExpenseCategory{}, "OwnerUserID"},
		{"Expense", Expense{}, "OwnerUserID"},
		{"File", File{}, "OwnerUserID"},
		{"AuditLog", AuditLog{}, "UserID"},
	}
	for _, c := range cases {
		typ := reflect.TypeOf(c.target)
		f, ok := typ.FieldByName(c.field)
		if !ok {
			t.Errorf("%s 缺少归属字段 %s，不变式 3「归属校验无死角」失去载体", c.entity, c.field)
			continue
		}
		if f.Type != typInt64 {
			t.Errorf("%s.%s 应为 int64（与 User.ID 同类型）: got %s", c.entity, c.field, f.Type)
		}
	}

	// User 自身：ID 即归属主体
	if f, ok := reflect.TypeOf(User{}).FieldByName("ID"); !ok || f.Type != typInt64 {
		t.Error("User.ID 应为 int64，它是全部业务数据的归属键")
	}
}

// TestConsumerStoreSoftDeleteAndPiercing 走查 store 的两处查询口径。
//
// ① 前提 8 / F-8：软删除只写 DeletedAt，**已删除行跳过、不覆盖其原值**（幂等）；
// ② 8.2：摊分区间穿刺 = 起始月 ≤ M 且 结束月 ≥ M。
func TestConsumerStoreSoftDeleteAndPiercing(t *testing.T) {
	t.Run("软删除幂等：已删除行不覆盖删除时间", func(t *testing.T) {
		first := time.Date(2026, 3, 1, 10, 0, 0, 0, time.Local)
		second := time.Date(2026, 4, 1, 10, 0, 0, 0, time.Local)

		// 模拟 store 的按 ID 软删除：已删除行跳过
		softDelete := func(e Expense, at time.Time) (Expense, bool) {
			if !e.DeletedAt.IsZero() {
				return e, false // 已删除，跳过且不覆盖
			}
			e.DeletedAt = at
			return e, true
		}

		e := Expense{ID: 260907000000000001}
		e, affected := softDelete(e, first)
		if !affected || e.DeletedAt != first {
			t.Fatal("首次删除应写入删除时间并计入受影响笔数")
		}
		e, affected = softDelete(e, second)
		if affected {
			t.Error("重复删除不应计入受影响笔数（F-8：已删除行跳过）")
		}
		if e.DeletedAt != first {
			t.Error("重复删除不得覆盖原删除时间（8.5），否则批次撤销的时间线被改写")
		}
	})

	t.Run("区间穿刺：起始月 ≤ M 且 结束月 ≥ M", func(t *testing.T) {
		e := Expense{
			AmortizeStart: calendar.MustParseMonth("2026-01"),
			AmortizeEnd:   calendar.MustParseMonth("2026-03"),
		}
		pierce := func(e Expense, m calendar.Month) bool {
			return !e.AmortizeStart.After(m) && !e.AmortizeEnd.Before(m)
		}
		for _, c := range []struct {
			month string
			hit   bool
		}{
			{"2025-12", false},
			{"2026-01", true},
			{"2026-02", true},
			{"2026-03", true},
			{"2026-04", false},
		} {
			if got := pierce(e, calendar.MustParseMonth(c.month)); got != c.hit {
				t.Errorf("穿刺 %s: got %t, want %t", c.month, got, c.hit)
			}
		}
	})
}

// ---------- L5 服务层 ----------

// TestConsumerFxUnconvergedRow 走查 service/fx（L5.2）的 I-7。
//
// 分层 §九 不变式 2：「**「未收敛行的识别」与「重算时的跳过」是同一个判据**：
// 折算本位币币种 ≠ / = User.本位币」。要求 Expense.BaseCurrency 与
// User.BaseCurrency 可直接比较。
func TestConsumerFxUnconvergedRow(t *testing.T) {
	user := User{ID: 260907000000000001, BaseCurrency: currency.MustParse("CNY")}

	// 同一判据的两种用法：I-7 跳过 / F-4 筛选识别
	converged := func(e Expense, u User) bool { return e.BaseCurrency.Equal(u.BaseCurrency) }

	t.Run("已收敛行被 I-7 跳过", func(t *testing.T) {
		e := Expense{BaseCurrency: currency.MustParse("CNY")}
		if !converged(e, user) {
			t.Error("折算本位币币种 = 当前本位币 的行应被判为已收敛并跳过（I-7）")
		}
	})

	t.Run("未收敛行可被唯一识别", func(t *testing.T) {
		e := Expense{BaseCurrency: currency.MustParse("USD")}
		if converged(e, user) {
			t.Error("折算本位币币种 ≠ 当前本位币 的行应被识别为待重算（不变式 2）")
		}
	})

	t.Run("已软删除明细同样参与重算", func(t *testing.T) {
		// I-7 明写「含已软删除的明细」——它们可经 F-4 查看、可被 F-8 复制，
		// 展示与复制初值都必须处于当前本位币口径。本包不设任何阻止其被重算的字段。
		e := Expense{
			BaseCurrency: currency.MustParse("USD"),
			DeletedAt:    time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local),
		}
		if converged(e, user) {
			t.Error("已软删除明细也应按同一判据识别为待重算（I-7）")
		}
	})
}

// TestConsumerAuditFirstThenAttach 走查 service/audit（L5.0）与不变式 4。
//
// 「一次提交 = 一条审计 = 一个可追溯批次」：审计ID 由 base/idgen 生成、
// **不依赖自增回填**，因此在事务提交前就能挂到明细与文件上。
func TestConsumerAuditFirstThenAttach(t *testing.T) {
	// 审计ID 在此直接用常量模拟 idgen 的返回值（本包不依赖 idgen，见依赖边界）
	const auditID int64 = 260907204907000001

	t.Run("数据入库：一条审计 + N 条明细挂同一批次号", func(t *testing.T) {
		audit := AuditLog{
			ID:         auditID,
			UserID:     260907000000000001,
			Type:       enum.OperationTypeDataIntake,
			ObjectType: enum.ObjectType(""), // 零值：批量操作（8.4 第 6 类）
			ObjectID:   0,
			Summary:    "导入 37 笔，合计 1,204.50 CNY",
			Result:     enum.OperationResultSuccess,
			Time:       time.Now(),
		}
		expenses := []Expense{
			{ID: 260907204907000010, SourceAuditID: audit.ID},
			{ID: 260907204907000011, SourceAuditID: audit.ID},
		}
		for i, e := range expenses {
			if e.SourceAuditID != audit.ID {
				t.Errorf("明细[%d] 未挂上来源审计ID，批次追溯与撤销失去边界（不变式 4）", i)
			}
		}
		// 8.4 第 6 类的对象类型与对象ID 都是零值，判定须允许零值
		if !audit.ObjectType.IsZero() {
			t.Error("数据入库属批量操作，对象类型应为零值（§四 5）")
		}
		if audit.ObjectID != 0 {
			t.Error("数据入库属批量操作，对象ID 应为零值")
		}
	})

	t.Run("文件上传：File 挂的是另一条审计", func(t *testing.T) {
		// 终版 8.4：File 挂「文件上传」那条，Expense 挂「数据入库」那条
		uploadAudit := AuditLog{
			ID:         260907204907000002,
			Type:       enum.OperationTypeFileUpload,
			ObjectType: enum.ObjectTypeFile,
			ObjectID:   260907204907000020,
			Result:     enum.OperationResultSuccess,
		}
		f := File{ID: 260907204907000020, SourceAuditID: uploadAudit.ID}
		if f.SourceAuditID == auditID {
			t.Error("File 应挂「文件上传」审计，不是「数据入库」审计（8.4）")
		}
		// C-3 双向关联：由审计可查回文件
		if uploadAudit.ObjectID != f.ID {
			t.Error("文件上传审计的对象ID 应指向该文件，C-3 双向可查依赖它")
		}
	})

	t.Run("登录失败：独立事务 + 有主则记", func(t *testing.T) {
		// 8.4 第 2 类：用户名存在则记录并归属该用户，不存在则不记审计
		failed := AuditLog{
			ID:         260907204907000003,
			UserID:     260907000000000001, // 被尝试账号的归属，不代表操作人
			Type:       enum.OperationTypeLogin,
			ObjectType: enum.ObjectType(""),
			Result:     enum.OperationResultFailure,
			Time:       time.Now(),
		}
		if failed.Result.IsSuccess() {
			t.Error("登录失败应以 操作结果 = 失败 表达（8.4 第 2 类）")
		}
		if failed.UserID == 0 {
			t.Error("有主的登录失败必须带上被尝试账号的归属，否则无消费方")
		}
	})
}

// TestConsumerIntakeCopyFieldTable 走查 service/intake（L5.4）的 F-8 复制。
//
// 终版 8.5 的复制字段表逐行走查：哪些随复制、哪些重算、哪些置空。
// 这是本包字段分组是否正确的最强校验——表里 5 行各自落在不同的字段组上。
func TestConsumerIntakeCopyFieldTable(t *testing.T) {
	// 一条已软删除的明细，作为被复制的原件
	src := Expense{
		// 业务七字段 + 折算汇率（随复制作初值，预览页可改）
		SpendDate:      calendar.MustParseDate("2026-01-15"),
		Amount:         decimal.MustParse("100.00"),
		Currency:       currency.MustParse("USD"),
		Counterparty:   "某商户",
		Remark:         "某备注",
		CategoryID:     260907000000000100,
		AmortizeMonths: 3,
		Rate:           decimal.MustParse("7.12345678"),
		// 卡片三属性 + 来源文件ID（随复制，不可改）
		BankName:     "某银行",
		CardLast4:    "1234",
		CardCurrency: currency.MustParse("USD"),
		SourceFileID: 260907000000000200,
		// 原件自身的状态
		ID:            260907000000000300,
		OwnerUserID:   260907000000000001,
		SourceAuditID: 260907000000000400,
		DeletedAt:     time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local),
	}

	// 模拟 service/intake 的复制：8.5 复制字段表
	const newExpenseID int64 = 260907000000000301
	const newAuditID int64 = 260907000000000401
	dst := src
	dst.ID = newExpenseID          // 新明细ID（idgen 生成）
	dst.SourceAuditID = newAuditID // = 本次复制提交产生的新审计
	dst.DeletedAt = time.Time{}    // 置空
	dst.BaseAmount = decimal.Decimal{}
	dst.BaseCurrency = currency.Code{}
	dst.AmortizeStart = calendar.Month{}
	dst.AmortizeEnd = calendar.Month{} // 四项只读派生：按 8.1/8.2 重算

	t.Run("业务七字段与折算汇率随复制", func(t *testing.T) {
		if !dst.SpendDate.Equal(src.SpendDate) || !dst.Amount.Equal(src.Amount) ||
			!dst.Currency.Equal(src.Currency) || dst.Counterparty != src.Counterparty ||
			dst.Remark != src.Remark || dst.CategoryID != src.CategoryID ||
			dst.AmortizeMonths != src.AmortizeMonths {
			t.Error("业务七字段应随复制作初值（8.5）")
		}
		// 复制**不重拉汇率**（终版 §十二 相对对话17 的实质变化①）
		if !dst.Rate.Equal(src.Rate) {
			t.Error("复制不重拉汇率，应沿用被复制明细的汇率作初值（8.1 末段）")
		}
	})

	t.Run("卡片三属性与来源文件ID 随复制且不可改", func(t *testing.T) {
		if dst.BankName != src.BankName || dst.CardLast4 != src.CardLast4 ||
			!dst.CardCurrency.Equal(src.CardCurrency) || dst.SourceFileID != src.SourceFileID {
			t.Error("卡片三属性与来源文件ID 应随复制继承（8.5：同一条数据，来源相同）")
		}
	})

	t.Run("来源审计ID 换成新审计，删除时间置空", func(t *testing.T) {
		if dst.SourceAuditID == src.SourceAuditID {
			t.Error("复制应挂到本次复制提交产生的新审计上（8.5）")
		}
		if !dst.DeletedAt.IsZero() {
			t.Error("复制出的新明细的删除时间应为空（8.5）")
		}
	})

	t.Run("四项只读派生待重算", func(t *testing.T) {
		if !dst.BaseAmount.IsZero() || !dst.BaseCurrency.IsZero() ||
			!dst.AmortizeStart.IsZero() || !dst.AmortizeEnd.IsZero() {
			t.Error("四项只读派生应按 8.1/8.2 重算而非直接继承")
		}
	})

	t.Run("手工录入时来源文件ID 为零值", func(t *testing.T) {
		// D-4 三种录入方式之一：手工录入，无来源文件
		manual := Expense{ID: 260907000000000302, SourceAuditID: newAuditID}
		if manual.SourceFileID != 0 {
			t.Error("手工录入的来源文件ID 应为零值（§四 3）")
		}
	})
}

// ---------- L6 接入层 ----------

// TestConsumerDtoPremise6 走查 api/http/dto（L6.0）与前提 6。
//
// 分层 §九 前提 6 行：「明细写请求的可写字段全集**恒等于 F-1 八项**；四项只读
// 派生在写请求结构里**根本不存在**（结构缺席 > 运行时忽略）」。
//
// 本包的责任是把这条边界**表达出来**：三段分组必须泾渭分明，dto 才能照着裁。
// 若可编辑组里混进 BaseAmount，dto 那一层就没有依据知道它不该出现在写请求里。
func TestConsumerDtoPremise6(t *testing.T) {
	editable := make(map[string]bool, len(expenseEditableFields))
	for _, f := range expenseEditableFields {
		editable[f.name] = true
	}

	t.Run("四项只读派生不在可编辑组内", func(t *testing.T) {
		for _, f := range expenseDerivedFields {
			if editable[f.name] {
				t.Errorf("只读派生字段 %s 混入了可编辑组，前提 6 的 dto 裁剪失去依据", f.name)
			}
		}
	})

	t.Run("十项系统只读不在可编辑组内", func(t *testing.T) {
		for _, f := range expenseSystemFields {
			if editable[f.name] {
				t.Errorf("系统只读字段 %s 混入了可编辑组", f.name)
			}
		}
	})

	t.Run("可编辑组恰为 F-1 八项且无重复", func(t *testing.T) {
		if len(editable) != 8 {
			t.Errorf("可编辑字段去重后应为 8 项: got %d", len(editable))
		}
		// F-1 的八项逐个点名（终版 F-1 原文顺序）
		want := []string{
			"SpendDate", "Amount", "Currency", "Counterparty",
			"Remark", "CategoryID", "AmortizeMonths", "Rate",
		}
		for _, name := range want {
			if !editable[name] {
				t.Errorf("F-1 的 %s 不在可编辑组内", name)
			}
		}
	})
}

// TestConsumerDtoPasswordRedline 走查分层 §九 的密码红线。
//
// 红线原文：「密码哈希 与 盐 **连三类出口都不得出现**——dto 不定义、A-7 列表裁剪、
// K-3 导出裁剪」。三处都是运行期约定，本包用 `json:"-"` 给「万一有人直接
// json.Marshal 了 entity.User」这条路径补一道兜底。
func TestConsumerDtoPasswordRedline(t *testing.T) {
	u := User{
		ID:           260907000000000001,
		Username:     "admin",
		PasswordHash: "argon2id_v1$超级机密的哈希值",
		Salt:         "超级机密的盐",
		Role:         enum.RoleAdmin,
		Status:       enum.UserStatusEnabled,
		BaseCurrency: currency.MustParse("CNY"),
		Language:     "zh-CN",
	}

	t.Run("json 序列化不含密码哈希与盐", func(t *testing.T) {
		data, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("序列化异常: %v", err)
		}
		text := string(data)
		for _, secret := range []string{u.PasswordHash, u.Salt, "PasswordHash", "Salt"} {
			if strings.Contains(text, secret) {
				t.Errorf("JSON 中出现了密码相关内容 %q，违反分层 §九 密码红线\n实际输出: %s", secret, text)
			}
		}
		// 反向确认序列化确实工作了，不是因为整体失败才「不含」
		if !strings.Contains(text, `"Username":"admin"`) {
			t.Errorf("JSON 应含用户名，实际输出: %s", text)
		}
	})

	t.Run("File.Password 刻意不加 json:\"-\"", func(t *testing.T) {
		// 密码红线管的是「用户登录密码」；File.Password 是文件解密口令，
		// 且 K-3 明写「File 的 文件内容 与 文件密码 照导」。
		f, ok := reflect.TypeOf(File{}).FieldByName("Password")
		if !ok {
			t.Fatal("File 缺少 Password 字段")
		}
		if f.Tag.Get("json") == "-" {
			t.Error("File.Password 不应加 json:\"-\"，K-3 明写导出包含文件密码")
		}
	})

	t.Run("除两处密码红线外无其他标签", func(t *testing.T) {
		// 包注释「三样刻意不做」第 1、2 条：不加 db/gorm 标签、不作 JSON 契约。
		// 允许的例外恰为 User.PasswordHash 与 User.Salt 的 json:"-"。
		cases := []struct {
			name   string
			target any
		}{
			{"User", User{}},
			{"ExpenseCategory", ExpenseCategory{}},
			{"Expense", Expense{}},
			{"File", File{}},
			{"AuditLog", AuditLog{}},
		}
		for _, c := range cases {
			typ := reflect.TypeOf(c.target)
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				tag := string(f.Tag)
				if tag == "" {
					continue
				}
				allowed := c.name == "User" && (f.Name == "PasswordHash" || f.Name == "Salt") &&
					tag == `json:"-"`
				if !allowed {
					t.Errorf("%s.%s 带有标签 %q：本包不加存储/契约标签（列名归 store/sqlite，契约归 dto）",
						c.name, f.Name, tag)
				}
			}
		}
	})
}

// TestConsumerDtoIDAsString 走查 idgen 的 JSON 精度结论在本包字段上的影响。
//
// base/idgen 的包注释给了实测：18 位 ID 超过 float64 安全整数上界 2^53，
// 经 JavaScript 的 Number 往返后会变成另一个数。本包的 8 个 int64 标识字段
// 全部受此影响，故 dto 必须以字符串传输。
//
// 本包不能自己解决（那需要加 json 标签、且 entity 不是契约），但可以把这条约束
// 变成可执行的提醒：一旦有人直接把 entity 当契约用，此处会指出后果。
func TestConsumerDtoIDAsString(t *testing.T) {
	const id int64 = 260907204907000001
	if float64(id) == float64(id+1) {
		// 这正是问题所在：两个不同的 ID 在 float64 下相等
		t.Logf("已确认 %d 与 %d 在 float64 下不可区分，dto 必须以字符串传输标识", id, id+1)
	} else {
		t.Errorf("预期 18 位 ID 在 float64 下精度丢失，实测未丢失（idgen 的结论需复核）")
	}

	// 点算本包的标识类字段，全部要求 dto 侧以字符串输出
	idFields := 0
	for _, c := range []any{User{}, ExpenseCategory{}, Expense{}, File{}, AuditLog{}} {
		typ := reflect.TypeOf(c)
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.Type == typInt64 && (f.Name == "ID" || strings.HasSuffix(f.Name, "ID")) {
				idFields++
			}
		}
	}
	// User.ID + Category(ID/OwnerUserID) + Expense(ID/OwnerUserID/SourceAuditID/
	// SourceFileID/CategoryID) + File(ID/OwnerUserID/SourceAuditID) +
	// AuditLog(ID/UserID/ObjectID) = 14
	if idFields != 14 {
		t.Errorf("标识类 int64 字段数: got %d, want 14（dto 需逐个以字符串传输）", idFields)
	}
}

// ---------- 依赖边界与零方法 ----------

// TestDependencyBoundary 锁定本包的 import 集合恰为分层 §六 L1.1 的依赖列。
//
// 依赖列：currency、enum、base/decimal、base/calendar。多一个都是问题：
//   - import 了 L2 及以上（rule / store / service / api）→ 反向依赖，必成环；
//   - import 了 base/errs → 零方法的包无错误可返，出现它意味着有人想加校验方法；
//   - import 了 base/idgen → 标识由 service 层生成后赋值，本包不该发号；
//   - import 了 i18n → L3，成环（Language 字段因此只能是 string，见其注释）。
func TestDependencyBoundary(t *testing.T) {
	const modulePrefix = "github.com/cellargalaxy/jotcash/"
	allowed := map[string]bool{
		"internal/currency":      true,
		"internal/enum":          true,
		"internal/base/decimal":  true,
		"internal/base/calendar": true,
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		// 只扫生产代码：测试文件本就可以 import 更多包（如本文件用了 encoding/json）
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析本包源码异常: %v", err)
	}

	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatalf("%s 的 import 路径无法解析: %s", name, imp.Path.Value)
				}
				if !strings.HasPrefix(path, modulePrefix) {
					continue // 标准库，本包只用 time
				}
				rel := strings.TrimPrefix(path, modulePrefix)
				seen[rel] = true
				if !allowed[rel] {
					t.Errorf("%s import 了 %s，超出分层 §六 L1.1 的依赖列（currency / enum / base/decimal / base/calendar）",
						name, rel)
				}
			}
		}
	}

	// 反向：四个依赖必须都真的用上了，否则说明有字段用了裸类型
	for dep := range allowed {
		if !seen[dep] {
			t.Errorf("依赖列中的 %s 未被 import：是否有字段该用它的类型却用了裸类型？", dep)
		}
	}
}

// TestEntitiesHaveNoMethod 锁定分层 §六 L1.1 的「**零方法**」。
//
// 准则 1「不变式内聚」要求一条不变式只有一处实现。一旦本包出现
// `func (e *Expense) Recalc()`，不变式 1a 就多出第二处实现，而这类新增在编译期
// 毫无阻力。故对 5 个类型的**值形态与指针形态双向**断言方法数为零。
//
// 双向是必要的：值形态的 NumMethod 不含指针接收者的方法，只查值形态会漏掉
// `func (e *Expense) ...`——而那恰恰是「就地修改实体」最常见的写法。
func TestEntitiesHaveNoMethod(t *testing.T) {
	cases := []struct {
		name  string
		value any
		ptr   any
	}{
		{"User", User{}, &User{}},
		{"ExpenseCategory", ExpenseCategory{}, &ExpenseCategory{}},
		{"Expense", Expense{}, &Expense{}},
		{"File", File{}, &File{}},
		{"AuditLog", AuditLog{}, &AuditLog{}},
	}
	for _, c := range cases {
		if n := reflect.TypeOf(c.value).NumMethod(); n != 0 {
			var names []string
			typ := reflect.TypeOf(c.value)
			for i := 0; i < n; i++ {
				names = append(names, typ.Method(i).Name)
			}
			sort.Strings(names)
			t.Errorf("%s 有 %d 个值方法 %v，违反分层 §六 L1.1「零方法」与准则 1",
				c.name, n, names)
		}
		if n := reflect.TypeOf(c.ptr).NumMethod(); n != 0 {
			var names []string
			typ := reflect.TypeOf(c.ptr)
			for i := 0; i < n; i++ {
				names = append(names, typ.Method(i).Name)
			}
			sort.Strings(names)
			t.Errorf("*%s 有 %d 个指针方法 %v，违反「零方法」——不变式会出现第二处实现",
				c.name, n, names)
		}
	}
}

// TestNoConstructorOrHelper 锁定本包只有类型声明，没有函数、变量与常量。
//
// 「零方法」的完整含义不止于方法：构造函数（NewExpense）、校验函数
// （ValidateExpense）、包级默认值同样会把规则带进 L1.1。合法性由字段类型自身
// 保证（currency.Code / calendar.Date / calendar.Month 私有字段 + Parse 构造），
// 不需要本包再加一层。
func TestNoConstructorOrHelper(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("解析本包源码异常: %v", err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					t.Errorf("%s 声明了函数 %s：本包只做类型声明，构造与校验归调用方",
						name, d.Name.Name)
				case *ast.GenDecl:
					switch d.Tok {
					case token.CONST:
						t.Errorf("%s 声明了常量：业务阈值一律写死在对应包（约定 8），不在 L1.1", name)
					case token.VAR:
						t.Errorf("%s 声明了包级变量：本包应无任何可变状态", name)
					}
				}
			}
		}
	}
}

// TestZeroValueUsable 锁定「零值即可用」这条被全部下游依赖的性质。
//
// 终版 §四 表头「不适用的场景以零值表达」+ 62 个字段全为值类型 ⇒
// `var e Expense` 就是一个可用的空明细，任何字段都能直接访问而不 panic。
// D-4 预览页的手工录入行、F-8 复制的初值都从这里起步。
func TestZeroValueUsable(t *testing.T) {
	t.Run("零值 Expense 的每个字段都可访问", func(t *testing.T) {
		var e Expense
		// 三类富类型的零值访问：不 panic，且各自 IsZero
		if !e.SpendDate.IsZero() || !e.AmortizeStart.IsZero() || !e.AmortizeEnd.IsZero() {
			t.Error("零值明细的日期与年月字段应均为零值")
		}
		if !e.Amount.IsZero() || !e.Rate.IsZero() || !e.BaseAmount.IsZero() {
			t.Error("零值明细的金额与汇率字段应均为零")
		}
		if !e.Currency.IsZero() || !e.CardCurrency.IsZero() || !e.BaseCurrency.IsZero() {
			t.Error("零值明细的币种字段应均为零值")
		}
		// 零值上取属性不得 panic——上层不应被迫先判空
		_ = e.Currency.Scale()
		_ = e.Currency.String()
		_ = e.SpendDate.ToMonth()
		_ = e.Amount.String()
		if !e.DeletedAt.IsZero() {
			t.Error("零值明细应为未删除状态")
		}
	})

	t.Run("零值 User 与 File 同理", func(t *testing.T) {
		var u User
		if !u.BaseCurrency.IsZero() || !u.LockedUntil.IsZero() || !u.LastLoginTime.IsZero() {
			t.Error("零值用户的可空语义字段应均为零值（未锁定、从未登录）")
		}
		if u.Role.Valid() || u.Status.IsEnabled() {
			t.Error("零值用户的角色与状态应均不合法（默认拒绝）")
		}
		var f File
		if f.Content != nil {
			t.Error("零值文件的内容应为 nil，表示「未加载」而非「空文件」")
		}
	})

	t.Run("含金额字段的实体不可用 == 比较", func(t *testing.T) {
		// decimal.Decimal 刻意不可比较，使 Expense 整体 == 成为编译错误，
		// 8.3 判重因此只能逐字段走 Equal。此处以反射确认该性质仍然成立。
		if reflect.TypeOf(Expense{}).Comparable() {
			t.Error("Expense 变成可比较了：== 会按内部表示比金额，8.3 判重将静默漏判")
		}
		// 反过来，不含金额的三个实体可比较，这没有害处
		for _, c := range []struct {
			name   string
			target any
		}{
			{"ExpenseCategory", ExpenseCategory{}},
			{"AuditLog", AuditLog{}},
		} {
			if !reflect.TypeOf(c.target).Comparable() {
				t.Errorf("%s 预期可比较（不含金额字段）", c.name)
			}
		}
	})
}
