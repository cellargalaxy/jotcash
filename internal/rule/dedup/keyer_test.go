package dedup

import (
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/entity"
)

// 本文件验证判重键的正确性，即 Check 全部结论所依赖的那条性质：
//
//	两行四要素相同 ⟺ 两行的判重键相同
//
// 键相等蕴含四要素相等（"⟸" 方向）保证不误报，四要素相等蕴含键相等
// （"⟹" 方向）保证不漏报。漏报比误报危险得多——误报只是多提示一行由人工排除，
// 漏报则是一笔重复账悄悄进了库，且此后任何统计都不会再暴露它。

// 测试中反复用到的固定取值。用具名常量而非各处写字面量，避免某处手滑改了
// 一个数字，使「本应相同」的两行变成不同而测试仍然通过。
const (
	userA int64 = 260907000000000001
	userB int64 = 260907000000000002
)

// newRow 构造一条只填四要素的明细。
//
// 其余字段一律留零值——这本身就是断言的一部分：若哪天有人把非四要素字段
// 纳入判重，本文件大量用例会因为「四要素相同的两行被判为不重复」而失败。
func newRow(owner int64, date, amount, code string) entity.Expense {
	return entity.Expense{
		OwnerUserID: owner,
		SpendDate:   calendar.MustParseDate(date),
		Amount:      decimal.MustParse(amount),
		Currency:    currency.MustParse(code),
	}
}

// TestAmountKeyIsCanonical 是本包最重要的一条断言：验证
// decimal.String() 确实是数值的规范形式，即
//
//	a.Equal(b)  ⟺  a.String() == b.String()
//
// 整个 Check 的 O(n+m) 实现建立在这条性质上。若它不成立，判重会静默漏判——
// 两笔金额相等的重复账因为文本不同而落进不同的 map 桶，谁也不会报错。
//
// 用**穷举交叉**而非挑几个例子：漏判的方向恰恰是「我没想到的那种写法」，
// 逐对验证才能覆盖到。样本刻意混入 Parse 与运算两条来源——运算结果不经 Parse，
// 若 decimal 只在 Parse 里做规范化而运算不做，只测 Parse 就发现不了。
func TestAmountKeyIsCanonical(t *testing.T) {
	texts := []string{
		"100.50", "100.5", "100.500", "+100.50", "1.005e2", // 同值五种写法
		"0", "-0", "0.00", "-0.00", "0e10", // 零的五种写法，含负零
		"-33.34", "-33.340", // 负金额（退款、冲正）
		"1e3", "1000", "1000.000", // 整数的三种写法
		"0.001",            // 3 位小数（KWD 这类币种）
		"1e-8",             // 汇率量级
		"9999999999999999", // 整数位上界
		"100.51",           // 相近但不同的值，作为对照
	}

	var samples []decimal.Decimal
	for _, text := range texts {
		d, err := decimal.Parse(text)
		if err != nil {
			t.Fatalf("解析 %q 报错: %v", text, err)
		}
		samples = append(samples, d)
	}

	// 混入非 Parse 来源：零值、聚合、运算结果、最小单位还原。
	// 这几种在真实链路里都会出现——零值来自空明细，运算结果来自 rule/convert
	// 与 rule/amortize，FromUnits 来自 store 回读。
	var zero decimal.Decimal
	samples = append(samples,
		zero,
		decimal.Sum(),
		decimal.FromInt(0),
		decimal.MustParse("0").Neg(), // 负零
		decimal.MustParse("100.50").Sub(decimal.MustParse("100.50")), // 算出来的零
		decimal.MustParse("100.5").Add(decimal.MustParse("0")),       // 加零不变
		decimal.MustParse("1.5").Mul(decimal.MustParse("2")),         // 3
		decimal.MustParse("1.55").Add(decimal.MustParse("0.45")),     // 2
	)
	if d, err := decimal.MustParse("100.5").Round(4, decimal.RoundHalfUp); err == nil {
		samples = append(samples, d) // 舍入到比自身更多的位数
	} else {
		t.Fatalf("构造舍入样本报错: %v", err)
	}
	if d, err := decimal.FromUnits(10050, 2); err == nil {
		samples = append(samples, d) // store 回读形态：10050 分 = 100.5
	} else {
		t.Fatalf("构造最小单位样本报错: %v", err)
	}

	// 穷举交叉：Equal 与「键相等」必须在每一对上都同进同退
	var mismatch int
	for i := range samples {
		for j := range samples {
			equal := samples[i].Equal(samples[j])
			sameKey := samples[i].String() == samples[j].String()
			if equal != sameKey {
				mismatch++
				t.Errorf("规范性被破坏: %q 与 %q  Equal=%t 但键相等=%t",
					samples[i].String(), samples[j].String(), equal, sameKey)
			}
		}
	}
	if mismatch == 0 {
		t.Logf("穷举 %d 对样本，Equal 与键相等完全一致", len(samples)*len(samples))
	}

	// 对照断言：确认样本里**确实存在**不相等的对，否则上面的循环可能因为
	// 「所有样本恰好都相等」而恒真，失去意义。
	if samples[0].Equal(decimal.MustParse("100.51")) {
		t.Fatal("样本构造有误：100.50 不应等于 100.51")
	}
}

// TestAmountKeyHandlesThreeDecimalCurrency 锁死一条容易写错的口径：
// 判重键中的金额是**原币**金额，位数随支出币种而定，不能按本位币的 2 位定点。
//
// 币种表刻意保留 KWD（3 位小数）正是为了让这条规则可被验证
// （见 currency/table.go「为什么保留 KWD」）。若判重键用
// StringFixed(decimal.BaseAmountStoreScale) 生成，KWD 金额会直接报错——
// base/decimal 的消费方走查里原本就是这么写的，已在本轮一并订正。
func TestAmountKeyHandlesThreeDecimalCurrency(t *testing.T) {
	kwd := currency.MustParse("KWD")
	if kwd.Scale() != 3 {
		t.Fatalf("KWD 小数位数应为 3，实得 %d（本用例的前提已不成立）", kwd.Scale())
	}
	if kwd.CanBeBase() {
		t.Error("KWD 不应是本位币候选（T38），这正是它的金额不能按 2 位定点建键的原因")
	}

	// 三位小数金额必须能正常建键
	row := newRow(userA, "2026-09-07", "0.001", "KWD")
	key := KeyOf(row)
	if key.Amount != "0.001" {
		t.Errorf("KWD 金额键: got %q, want %q", key.Amount, "0.001")
	}

	// 同值不同写法仍是同一个键
	same := newRow(userA, "2026-09-07", "0.00100", "KWD")
	if KeyOf(same).String() != key.String() {
		t.Error("0.001 与 0.00100 应生成同一个判重键")
	}

	// 而按本位币 2 位定点建键会直接失败——这是订正前的写法
	if _, err := decimal.MustParse("0.001").StringFixed(decimal.BaseAmountStoreScale); err == nil {
		t.Error("前提已变：3 位小数金额竟能被 2 位定点无损容纳，" +
			"若如此则本用例记录的缺陷不再成立，需重新评估口径")
	}
}

// TestKeyMatchesFourFactors 逐个要素验证「改一个要素就换一个键」。
//
// 这是判重不误报的基础：四要素中任何一项不同，两行就不是重复。
func TestKeyMatchesFourFactors(t *testing.T) {
	base := newRow(userA, "2026-09-07", "100.50", "CNY")
	baseKey := KeyOf(base).String()

	cases := []struct {
		name string
		row  entity.Expense
	}{
		{"归属用户不同", newRow(userB, "2026-09-07", "100.50", "CNY")},
		{"支出日期不同", newRow(userA, "2026-09-08", "100.50", "CNY")},
		{"支出金额不同", newRow(userA, "2026-09-07", "100.51", "CNY")},
		{"支出币种不同", newRow(userA, "2026-09-07", "100.50", "USD")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if KeyOf(c.row).String() == baseKey {
				t.Errorf("%s 时键不应相同（否则会把两笔不同的支出误判为重复）", c.name)
			}
		})
	}

	// 金额相差一个最小分位也必须区分开——这是最容易被「先舍入再建键」
	// 这类写法破坏的一条：若建键前把金额舍到 1 位，100.50 与 100.51 会撞键。
	near := newRow(userA, "2026-09-07", "100.51", "CNY")
	if KeyOf(near).String() == baseKey {
		t.Error("100.50 与 100.51 相差一分，不应撞键")
	}
}

// TestKeyIgnoresNonFactorFields 锁死 E-1 的排除项：
// 「不设指纹属性，不纳入卡片、对手方、备注等非必有字段」。
//
// 反向价值在于：若哪天有人「顺手」把对手方加进判重，同一份账单重复导入时
// 会因为一个备注差异而漏判——而漏判是无声的。
func TestKeyIgnoresNonFactorFields(t *testing.T) {
	base := newRow(userA, "2026-09-07", "100.50", "CNY")

	// 把全部非四要素字段都改掉，键必须不变
	noisy := base
	noisy.ID = 260907000000000009
	noisy.Counterparty = "另一个商户"
	noisy.Remark = "另一份备注"
	noisy.CategoryID = 260907000000000003
	noisy.AmortizeMonths = 12
	noisy.Rate = decimal.MustParse("7.12345678")
	noisy.BaseAmount = decimal.MustParse("715.86")
	noisy.BaseCurrency = currency.MustParse("CNY")
	noisy.AmortizeStart = calendar.MustParseMonth("2026-09")
	noisy.AmortizeEnd = calendar.MustParseMonth("2027-08")
	noisy.SourceAuditID = 260907000000000004
	noisy.SourceFileID = 260907000000000005
	noisy.BankName = "某某银行"
	noisy.CardLast4 = "9999"
	noisy.CardCurrency = currency.MustParse("HKD")

	if KeyOf(noisy).String() != KeyOf(base).String() {
		t.Error("非四要素字段不应影响判重键（E-1：不纳入卡片、对手方、备注等非必有字段）")
	}

	// 正向确认：这些字段确实被改动了，否则上面的断言恒真
	if noisy.Counterparty == base.Counterparty && noisy.CardLast4 == base.CardLast4 {
		t.Fatal("用例构造有误：非四要素字段并未真正改动")
	}
}

// TestKeySeparatorNoCollision 验证四要素拼接不会串键。
//
// 拼接式键的经典缺陷是相邻字段串位：「110」+「0.5」与「1」+「100.5」在不加
// 分隔符时都拼成 1100.5，两笔毫不相干的支出会被判为重复。本用例用一对
// **刻意构造的串位取值**把分隔符的必要性钉死——去掉 keySep 时它必然失败。
//
// 同时覆盖零值（空串）参与拼接的情形：零值日期与零值币种都产生空串，
// 是最容易串位的一组。
func TestKeySeparatorNoCollision(t *testing.T) {
	// 零值日期与零值币种都会产生空串，让相邻的用户ID 与金额直接贴在一起
	var zeroDate calendar.Date
	var zeroCode currency.Code

	// 这两行去掉分隔符后都拼成 1100.5，但它们是两个不同用户的不同金额
	collidingA := entity.Expense{OwnerUserID: 110, SpendDate: zeroDate,
		Amount: decimal.MustParse("0.5"), Currency: zeroCode}
	collidingB := entity.Expense{OwnerUserID: 1, SpendDate: zeroDate,
		Amount: decimal.MustParse("100.5"), Currency: zeroCode}

	if KeyOf(collidingA).String() == KeyOf(collidingB).String() {
		t.Errorf("串键：用户110/金额0.5 与 用户1/金额100.5 得到同一个键 %q。\n"+
			"四要素拼接必须有分隔符，否则相邻字段会串位，"+
			"两笔毫不相干的支出会被判为重复。", KeyOf(collidingA).String())
	}

	// 更完整的一组：全零值、部分零值、正常行两两之间都不得撞键
	rows := []entity.Expense{
		collidingA,
		collidingB,
		// 全零值：三个空串相邻
		{OwnerUserID: userA, SpendDate: zeroDate, Amount: decimal.MustParse("0"), Currency: zeroCode},
		// 只有币种为零值
		{OwnerUserID: userA, SpendDate: calendar.MustParseDate("2026-09-07"),
			Amount: decimal.MustParse("0"), Currency: zeroCode},
		// 只有日期为零值
		{OwnerUserID: userA, SpendDate: zeroDate,
			Amount: decimal.MustParse("0"), Currency: currency.MustParse("CNY")},
		// 正常行
		newRow(userA, "2026-09-07", "0", "CNY"),
	}

	seen := make(map[string]int)
	for i, row := range rows {
		key := KeyOf(row).String()
		if prev, dup := seen[key]; dup {
			t.Errorf("第 %d 行与第 %d 行串键: %q", i, prev, key)
			continue
		}
		seen[key] = i
	}
	if len(seen) != len(rows) {
		t.Errorf("%d 行应产生 %d 个不同的键，实得 %d", len(rows), len(rows), len(seen))
	}
}

// TestKeyOfExposesFourFactors 验证 Key 的四个字段确实取自四要素，
// 且形态与 store 落库形态一致（store 要拿它拼 SQL 元组条件）。
func TestKeyOfExposesFourFactors(t *testing.T) {
	row := newRow(userA, "2026-09-07", "100.50", "CNY")
	key := KeyOf(row)

	if key.OwnerUserID != userA {
		t.Errorf("归属用户: got %d, want %d", key.OwnerUserID, userA)
	}
	// 日期形态须与 calendar.Date 的落库文本一致（定长 10 字符）
	if key.SpendDate != "2026-09-07" {
		t.Errorf("支出日期: got %q, want %q", key.SpendDate, "2026-09-07")
	}
	if len(key.SpendDate) != len(calendar.DateLayout) {
		t.Errorf("支出日期文本应定长 %d 字符，实得 %d", len(calendar.DateLayout), len(key.SpendDate))
	}
	// 金额是规范文本，剥掉尾随零
	if key.Amount != "100.5" {
		t.Errorf("支出金额: got %q, want %q", key.Amount, "100.5")
	}
	// 币种是代码本身，与 currency.Code 的落库形态一致
	if key.Currency != "CNY" {
		t.Errorf("支出币种: got %q, want %q", key.Currency, "CNY")
	}

	// Key 必须可比较——store 侧要拿它作 map 键去重
	if KeyOf(row) != key {
		t.Error("Key 应可比较且同一明细算出的键相等")
	}
}

// TestKeysDeduplicatesAndKeepsOrder 验证 Keys 的两条性质：
// 去重（同键只出一次）与保序（按首次出现顺序）。
//
// 去重是为了让 store 的一次批量查询不带重复条件；保序是为了让生成的 SQL
// 参数顺序稳定，便于日志比对。
func TestKeysDeduplicatesAndKeepsOrder(t *testing.T) {
	rows := []entity.Expense{
		newRow(userA, "2026-09-07", "100.50", "CNY"),  // 第 1 个唯一键
		newRow(userA, "2026-09-08", "200.00", "CNY"),  // 第 2 个唯一键
		newRow(userA, "2026-09-07", "100.5", "CNY"),   // 与第 1 行同键（写法不同）
		newRow(userA, "2026-09-09", "300.00", "USD"),  // 第 3 个唯一键
		newRow(userA, "2026-09-08", "200.000", "CNY"), // 与第 2 行同键
	}

	keys := Keys(rows)
	if len(keys) != 3 {
		t.Fatalf("5 行含 3 个唯一键，Keys 实得 %d 个", len(keys))
	}

	want := []struct{ date, amount, code string }{
		{"2026-09-07", "100.5", "CNY"},
		{"2026-09-08", "200", "CNY"},
		{"2026-09-09", "300", "USD"},
	}
	for i, w := range want {
		if keys[i].SpendDate != w.date || keys[i].Amount != w.amount || keys[i].Currency != w.code {
			t.Errorf("第 %d 个键: got (%s, %s, %s), want (%s, %s, %s)",
				i, keys[i].SpendDate, keys[i].Amount, keys[i].Currency, w.date, w.amount, w.code)
		}
	}

	// 空入参返回空切片而非 nil 崩溃
	if got := Keys(nil); len(got) != 0 {
		t.Errorf("空入参应返回空集合，实得 %d 个", len(got))
	}
}
