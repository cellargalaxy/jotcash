package currency_test

// 外部消费者验证：以**黑盒**方式（只用导出 API，包名为 currency_test）模拟本包的真实调用方
// ——`rule/convert` 的不变式 1a 舍入基准、`rule/amortize` 的 8.2 按币种小数位均分、
// `service/preference` 的 K-1 本位币候选校验（T38）、`store/sqlite` 的币种列往返、
// `api/http/dto` 的 JSON 契约（T34②）、`fxrate` 注册表的按标识路由。
//
// 文件命名与 `base/decimal`、`base/calendar`、`base/config`、`base/pwdhash` 既有的
// zz_consumer_check_test.go 约定一致。
//
// 与白盒 currency_test.go 的分工：那边验策展表自身与各判据的分工（可访问 table / byCode），
// 这边只走导出面，验「下游真的能用它把活干完」——尤其是本包与 `base/decimal` 的接缝：
// Currency.Scale 取 int32 就是为了直喂 decimal.RoundAmount，这条接缝只有黑盒才测得到。

import (
	"encoding/json"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// TestConvertRoundingBasisAsRuleConvert 模拟 `rule/convert` 的不变式 1a：
// 「舍入基准 = **本位币的** currency.小数位数」（分层 §三 问题 1 的修法）。
//
// 这条是分层文档里被专门修复过的一处：原承载把舍入基准写死为 2 位，本位币为 0 位币种（JPY）时
// 会算出该币种不存在的面额。本用例就按修法后的口径跑一遍 JPY 本位币，确认
// 「Scale 直接喂给 RoundAmount」这条路走得通、且产出的是整数面额。
func TestConvertRoundingBasisAsRuleConvert(t *testing.T) {
	// 一笔 USD（2 位）支出，折算为 JPY（0 位）本位币
	amount, err := decimal.NewFromString("100.10")
	if err != nil {
		t.Fatalf("原币金额解析异常: %+v", err)
	}
	rawRate, err := decimal.NewFromString("155.1234567891") // 汇率源返回 10 位
	if err != nil {
		t.Fatalf("汇率解析异常: %+v", err)
	}
	rate, err := decimal.RoundRate(rawRate) // T39：先规整 8 位
	if err != nil {
		t.Fatalf("汇率规整异常: %+v", err)
	}

	baseCur, ok := currency.Get(currency.JPY)
	if !ok {
		t.Fatal("JPY 应在枚举表内")
	}
	if !baseCur.SupportsBase() {
		t.Fatal("JPY（0 位）应可作本位币（T38：≤ 2 位）")
	}

	product, err := amount.Mul(rate)
	if err != nil {
		t.Fatalf("折算相乘异常: %+v", err)
	}
	// **接缝所在**：Currency.Scale 直接作为 RoundAmount 的入参，无需类型转换
	baseAmount, err := decimal.RoundAmount(product, baseCur.Scale)
	if err != nil {
		t.Fatalf("本位币金额舍入异常: %+v", err)
	}
	// 0 位币种的结果必须是整数面额——这正是修复前会出错的地方
	if got := baseAmount.Scale(); got > 0 {
		t.Errorf("JPY 本位币金额小数位 = %d，期望 0（0 位币种不存在小数面额）", got)
	}
	// 且必须能按 2 位存储列无损容纳（T38 保证 ≤ 2 位 → 存 2 位无损）
	if _, err := decimal.StoreBaseAmount(baseAmount); err != nil {
		t.Errorf("0 位本位币金额应能无损存入 2 位定点列: %+v", err)
	}
	t.Logf("USD %v × %v → JPY %v（按本位币 %d 位舍入）",
		amount.String(), rate.String(), baseAmount.String(), baseCur.Scale)

	// 对照：同一笔折算为 CNY（2 位）本位币，结果应保留 2 位
	cnyCur := currency.MustGet(currency.CNY)
	cnyAmount, err := decimal.RoundAmount(product, cnyCur.Scale)
	if err != nil {
		t.Fatalf("CNY 舍入异常: %+v", err)
	}
	if cnyAmount.Scale() > 2 {
		t.Errorf("CNY 本位币金额小数位 = %d，期望 ≤ 2", cnyAmount.Scale())
	}
	// 两个本位币口径下的结果必然不同——若相同说明 Scale 没被真正用上
	if baseAmount.Equal(cnyAmount) {
		t.Error("JPY 与 CNY 两种本位币口径的折算结果不应相同（否则 Scale 未参与舍入）")
	}
}

// TestAmortizeBasisAsRuleAmortize 模拟 `rule/amortize` 的 8.2：
// 「按币种的小数位数均分，除不尽的尾差固定计入末月」，原币与本位币各自独立均分。
//
// 覆盖 0 / 2 / 3 三档小数位，其中 3 位（KWD）刻意验证「T38 只约束本位币、
// 不约束支出币种」——KWD 作支出币种必须能正常按 3 位均分。
func TestAmortizeBasisAsRuleAmortize(t *testing.T) {
	const months = 7
	cases := []struct {
		code  currency.Code
		total string
	}{
		{currency.CNY, "1000.01"},  // 2 位
		{currency.JPY, "160837"},   // 0 位
		{currency.KWD, "1000.001"}, // 3 位：支出币种不受 T38 限制
		{currency.CNY, "-1000.01"}, // 负金额（退款）对称
	}
	for _, c := range cases {
		scale, ok := currency.Scale(c.code)
		if !ok {
			t.Errorf("%s 应可查到小数位数", c.code)
			continue
		}
		total, err := decimal.NewFromString(c.total)
		if err != nil {
			t.Errorf("%s 总额解析异常: %+v", c.code, err)
			continue
		}
		// 按**该币种**的小数位数均分（不是存储用的 2 位）
		per, err := total.DivTruncate(decimal.NewFromInt(months), scale)
		if err != nil {
			t.Errorf("%s 均分异常: %+v", c.code, err)
			continue
		}
		// 每月金额必须能按该币种位数无损表示
		if _, err := decimal.FixedString(per, scale); err != nil {
			t.Errorf("%s 每月金额 %v 无法按 %d 位无损表示: %+v", c.code, per.String(), scale, err)
			continue
		}
		sum := decimal.Zero()
		for i := 0; i < months-1; i++ {
			sum = sum.Add(per)
		}
		last := total.Sub(sum) // 尾差归末月
		if !sum.Add(last).Equal(total) {
			t.Errorf("%s 逐月之和 %v 应恒等于总额 %v", c.code, sum.Add(last).String(), total.String())
		}
		if _, err := decimal.FixedString(last, scale); err != nil {
			t.Errorf("%s 末月金额 %v 无法按 %d 位无损表示: %+v", c.code, last.String(), scale, err)
		}
		t.Logf("%s（%d 位）：总额 %v，每月 %v，末月 %v", c.code, scale, total.String(), per.String(), last.String())
	}
}

// TestBaseCandidatesAsServicePreference 模拟 `service/preference` 的 K-1 本位币切换：
// 「本位币走 currency 的『本位币候选』校验（T38，非候选一律拒绝）」。
func TestBaseCandidatesAsServicePreference(t *testing.T) {
	candidates := currency.BaseCandidates()
	if len(candidates) == 0 {
		t.Fatal("本位币候选集不应为空（否则 K-1 下拉无可选项）")
	}
	// 候选集首位应是 CNY——I-2 默认本位币，K-1 下拉首选
	if candidates[0].Code != currency.CNY {
		t.Errorf("候选集首位应为 CNY（I-2 默认本位币），实际 %s", candidates[0].Code)
	}
	// 候选集每一项都必须真的通过 ValidateBase——集合与单值校验不得自相矛盾
	// （这正是把口径收成一个具名判据要防的那类「下拉里能选、保存时被拒」）
	for _, cur := range candidates {
		if err := currency.ValidateBase(cur.Code); err != nil {
			t.Errorf("候选集内的 %s 未通过 ValidateBase: %+v", cur.Code, err)
		}
		if cur.Scale > 2 {
			t.Errorf("候选集内的 %s 小数位数 %d > 2，违反 T38", cur.Code, cur.Scale)
		}
		if !cur.Enabled {
			t.Errorf("候选集内的 %s 未启用，违反 T38", cur.Code)
		}
	}
	// 默认本位币 CNY 必须在候选集内，否则 A-1 初始化就建不出账户
	if !currency.CanBeBase(currency.CNY) {
		t.Error("默认本位币 CNY 必须在候选集内（A-1 初始化依赖它）")
	}

	// 3 位币种：可作支出币种，但不可作本位币，且错误档位可区分
	for _, code := range []currency.Code{currency.KWD, currency.BHD, currency.OMR, currency.JOD} {
		if currency.CanBeBase(code) {
			t.Errorf("%s（3 位）不应可作本位币", code)
		}
		// 但作支出币种完全正常
		if err := currency.ValidateEnabled(code); err != nil {
			t.Errorf("%s 作支出币种应正常: %+v", code, err)
		}
		// 错误必须是「不可作本位币」而非「币种非法」——用户需要知道这个区别
		err := currency.ValidateBase(code)
		if err == nil {
			t.Errorf("%s 作本位币应失败", code)
			continue
		}
		e, ok := err.(*errs.Error)
		if !ok {
			t.Errorf("%s 应返回 *errs.Error，实际 %T", code, err)
			continue
		}
		if e.Key() != errs.KeyCurrencyNotBaseCandidate {
			t.Errorf("%s 的文案键应为 %s，实际 %s", code, errs.KeyCurrencyNotBaseCandidate, e.Key())
		}
		if e.Kind() != errs.KindInput {
			t.Errorf("%s 的错误档位应为 KindInput，实际 %v", code, e.Kind())
		}
		// 占位参数须带币种代码，否则文案渲染不出「哪个币种不能当本位币」
		if got := e.Args()[errs.ArgCurrency]; got != code.String() {
			t.Errorf("%s 的占位参数 %s 应为 %s，实际 %v", code, errs.ArgCurrency, code, got)
		}
	}
}

// TestStoreRoundTripAsStoreSqlite 模拟 `store/sqlite` 的币种列往返：
// 落库走 driver.Valuer（字符串 / NULL），读回走 sql.Scanner。
func TestStoreRoundTripAsStoreSqlite(t *testing.T) {
	// 必填字段（支出币种 / 折算本位币币种）：落字符串、原样读回
	for _, code := range []currency.Code{currency.CNY, currency.JPY, currency.KWD} {
		v, err := code.Value()
		if err != nil {
			t.Errorf("%s 落库异常: %+v", code, err)
			continue
		}
		text, ok := v.(string)
		if !ok {
			t.Errorf("%s 应落 string，实际 %T（终版 §五 1「库中只存币种代码」）", code, v)
			continue
		}
		var back currency.Code
		if err := back.Scan(text); err != nil {
			t.Errorf("%s 读回异常: %+v", code, err)
			continue
		}
		if back != code {
			t.Errorf("%s 往返后不等: %s → %q → %s", code, code, text, back)
		}
	}

	// 非必填字段（卡片币种「解析不出则留空」）：零值 ↔ NULL 严格互逆
	var zero currency.Code
	v, err := zero.Value()
	if err != nil {
		t.Fatalf("零值落库异常: %+v", err)
	}
	if v != nil {
		t.Errorf("零值应落 NULL 而非 %#v（空串会让『留空』与『存了空值』在 SQL 层不可区分）", v)
	}
	var back currency.Code
	if err := back.Scan(nil); err != nil {
		t.Fatalf("NULL 读回异常: %+v", err)
	}
	if !back.IsZero() {
		t.Errorf("NULL 应读回零值，实际 %q", back)
	}

	// 驱动可能返回 []byte，两种文本类型都要收
	var fromBytes currency.Code
	if err := fromBytes.Scan([]byte("CNY")); err != nil {
		t.Errorf("[]byte 读回异常: %+v", err)
	} else if fromBytes != currency.CNY {
		t.Errorf("[]byte 读回 = %s，期望 CNY", fromBytes)
	}

	// 库里出现未知代码 → KindSystem（数据异常），**不是** KindInput
	var bad currency.Code
	err = bad.Scan("ZZZ")
	if err == nil {
		t.Fatal("库中未知币种代码应读取失败")
	}
	if e, ok := err.(*errs.Error); !ok {
		t.Errorf("应返回 *errs.Error，实际 %T", err)
	} else if e.Kind() != errs.KindSystem {
		t.Errorf("库中数据异常的档位应为 KindSystem，实际 %v"+
			"（档位错了会让这类严重问题在接口上表现为 4xx 被当成用户问题忽略）", e.Kind())
	}

	// 空串读回零值：可能来自本仓库之前的写入或外部导入，读侧一并容忍
	// （比让一行历史数据读不出来更合适，见 Scan 注释 ③）
	var fromEmpty currency.Code
	if err := fromEmpty.Scan(""); err != nil {
		t.Errorf("空串应容忍并读回零值，实际报错: %+v", err)
	} else if !fromEmpty.IsZero() {
		t.Errorf("空串应读回零值，实际 %q", fromEmpty)
	}
	// []byte 形态的空串同样容忍
	var fromEmptyBytes currency.Code
	if err := fromEmptyBytes.Scan([]byte("")); err != nil {
		t.Errorf("[]byte 空串应容忍并读回零值，实际报错: %+v", err)
	} else if !fromEmptyBytes.IsZero() {
		t.Errorf("[]byte 空串应读回零值，实际 %q", fromEmptyBytes)
	}

	// 不支持的驱动类型 → KindSystem（同属数据 / 驱动异常，不是用户输入错误）
	var badType currency.Code
	err = badType.Scan(12345)
	if err == nil {
		t.Fatal("不支持的驱动类型应读取失败")
	}
	if e, ok := err.(*errs.Error); !ok {
		t.Errorf("应返回 *errs.Error，实际 %T", err)
	} else if e.Kind() != errs.KindSystem {
		t.Errorf("不支持的驱动类型档位应为 KindSystem，实际 %v", e.Kind())
	}

	// []byte 形态的未知代码同样按 KindSystem 拒绝（与 string 分支口径一致）
	var badBytes currency.Code
	if err := badBytes.Scan([]byte("ZZZ")); err == nil {
		t.Error("[]byte 形态的未知币种代码应读取失败")
	} else if e, ok := err.(*errs.Error); ok && e.Kind() != errs.KindSystem {
		t.Errorf("[]byte 未知代码档位应为 KindSystem，实际 %v", e.Kind())
	}
}

// TestJSONContractAsDto 模拟 `api/http/dto` 的 JSON 契约（T34②）：
// 币种以字符串出入，非法值在**反序列化这一步**就被拦住。
func TestJSONContractAsDto(t *testing.T) {
	type writeReq struct {
		ExpenseCurrency currency.Code `json:"expenseCurrency"`
		CardCurrency    currency.Code `json:"cardCurrency"` // 非必填，可留空
	}

	// 合法请求：含 3 位支出币种（T38 不约束支出币种）与留空的卡片币种
	var req writeReq
	if err := json.Unmarshal([]byte(`{"expenseCurrency":"KWD","cardCurrency":""}`), &req); err != nil {
		t.Fatalf("合法请求应被接收: %+v", err)
	}
	if req.ExpenseCurrency != currency.KWD {
		t.Errorf("支出币种 = %s，期望 KWD", req.ExpenseCurrency)
	}
	if !req.CardCurrency.IsZero() {
		t.Errorf("卡片币种应为零值（留空），实际 %s", req.CardCurrency)
	}

	// 序列化：零值出空串（非必填留空是合法业务态），非零出代码
	raw, err := json.Marshal(writeReq{ExpenseCurrency: currency.CNY})
	if err != nil {
		t.Fatalf("序列化异常: %+v", err)
	}
	if got, want := string(raw), `{"expenseCurrency":"CNY","cardCurrency":""}`; got != want {
		t.Errorf("序列化 = %v, 期望 %v", got, want)
	}

	// 非法值一律在反序列化时拒收——不让一个查不到小数位数的币种走到 rule/amortize
	for _, body := range []string{
		`{"expenseCurrency":"ZZZ"}`,  // 未收录
		`{"expenseCurrency":"cny"}`,  // 小写（归一责任在 parser）
		`{"expenseCurrency":" CNY"}`, // 带空白
		`{"expenseCurrency":"CNH"}`,  // 非 ISO 4217
		`{"expenseCurrency":"XAU"}`,  // 贵金属，非流通货币
		`{"expenseCurrency":"CLF"}`,  // 4 位记账单位，刻意未收录
	} {
		var r writeReq
		if err := json.Unmarshal([]byte(body), &r); err == nil {
			t.Errorf("非法请求 %s 应被拒收", body)
		} else if e, ok := err.(*errs.Error); ok && e.Kind() != errs.KindInput {
			t.Errorf("%s 的错误档位应为 KindInput，实际 %v", body, e.Kind())
		}
	}
}

// TestFxSourceRoutingAsApp 模拟 `app` 按汇率源标识把 L4 实现注册进 `fxrate` 注册表：
// 每个币种都必须有可路由的源，且注册表侧能自检「注册了没人引用的源」。
func TestFxSourceRoutingAsApp(t *testing.T) {
	// app 侧的注册表：标识 → 实现（这里用桩函数代替 L4 实现体）
	registry := map[currency.FxSource]string{
		currency.FxSourceDefault: "fxrate/<源> 的桩实现",
	}
	// 全部币种（含停用项——I-7 重算会碰到它们）都必须能路由到已注册的源
	for _, cur := range currency.All() {
		if !cur.FxSource.Valid() {
			t.Errorf("%s 的汇率源标识 %q 未被本包收录", cur.Code, cur.FxSource)
			continue
		}
		if _, ok := registry[cur.FxSource]; !ok {
			t.Errorf("%s 的汇率源 %q 无对应注册项（fxrate 会路由到无人注册的源）", cur.Code, cur.FxSource)
		}
	}
	// 反向自检：注册了但无人引用的源属装配错误
	referenced := map[currency.FxSource]bool{}
	for _, cur := range currency.All() {
		referenced[cur.FxSource] = true
	}
	for src := range registry {
		if !referenced[src] {
			t.Errorf("汇率源 %q 已注册但无任何币种引用", src)
		}
	}
}

// TestReadVsWriteJudgementAsService 模拟服务层对「读侧 / 写侧」两个判据的分工：
// 读侧用 Valid（含停用项，历史明细要能读回），写侧用 Enabled（新录入可选集）。
//
// 这条分工是「枚举项只能停用不能删除」这条纪律能否兑现的关键：若读侧也按 Enabled 判，
// 停用一个币种就会让它的全部历史支出变成读不出来的行。本轮策展表全部启用，
// 故此处只能验证两个判据在**已启用**币种上一致、且各自的语义出口都存在；
// 停用分支的行为由白盒 TestValidVsEnabled 用临时注入项钉死。
func TestReadVsWriteJudgementAsService(t *testing.T) {
	all := currency.All()
	enabled := currency.EnabledAll()
	// 已启用集必须是全集的子集
	if len(enabled) > len(all) {
		t.Errorf("EnabledAll(%d) 不应多于 All(%d)", len(enabled), len(all))
	}
	for _, cur := range enabled {
		if !currency.Valid(cur.Code) {
			t.Errorf("已启用的 %s 必须同时是合法币种", cur.Code)
		}
		if !currency.Enabled(cur.Code) {
			t.Errorf("EnabledAll 内的 %s 未通过 Enabled 判据", cur.Code)
		}
	}
	// All 返回副本：调用方改不到本包的策展表（否则一次误改会全局改掉某币种的 Scale）
	if len(all) > 0 {
		all[0].Scale = 99
		if again := currency.All(); again[0].Scale == 99 {
			t.Error("All 应返回副本，调用方的修改不得影响策展表")
		}
	}
	// 零值不是币种，读写两侧都不认
	if currency.Valid(currency.Code("")) || currency.Enabled(currency.Code("")) {
		t.Error("零值不应被任何判据判为币种")
	}
}
