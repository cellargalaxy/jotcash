package currency

import (
	"encoding/json"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// 白盒单测：验证策展表自身的完整性与本包各判据的分工。
// 与 ISO 4217 的逐条比对在 table_iso_test.go，外部消费者验证在 zz_consumer_check_test.go。

// TestTableIntegrity 策展表的结构性自检。
// init 里已有同类 panic 校验，这里再断言一遍，是为了让「表写错了」表现为一条明确的失败用例，
// 而不是一句 panic 堆栈——后者在批量跑测时很难定位到具体哪一行写错。
func TestTableIntegrity(t *testing.T) {
	if len(table) == 0 {
		t.Fatal("策展表不得为空")
	}
	seen := map[Code]bool{}
	for _, cur := range table {
		if seen[cur.Code] {
			t.Fatalf("币种代码 %s 重复", cur.Code)
		}
		seen[cur.Code] = true

		if len(cur.Code) != 3 {
			t.Fatalf("币种代码 %s 不是三位", cur.Code)
		}
		for i := 0; i < len(cur.Code); i++ {
			if c := cur.Code[i]; c < 'A' || c > 'Z' {
				t.Fatalf("币种代码 %s 含非大写字母", cur.Code)
			}
		}
		if cur.Name == "" {
			t.Fatalf("币种 %s 缺名称", cur.Code)
		}
		if cur.Symbol == "" {
			t.Fatalf("币种 %s 缺符号", cur.Code)
		}
		if cur.Scale < 0 || cur.Scale > 4 {
			t.Fatalf("币种 %s 小数位数 %d 超出 [0,4]", cur.Code, cur.Scale)
		}
		if !cur.FxSource.Valid() {
			t.Fatalf("币种 %s 汇率源标识 %q 未收录", cur.Code, cur.FxSource)
		}
	}

	// byCode 必须与 table 完全同步（byCode 是 init 从 table 构建的，此处防将来有人手改其一）
	if len(byCode) != len(table) {
		t.Fatalf("byCode 与 table 数量不符: %d vs %d", len(byCode), len(table))
	}
	for _, cur := range table {
		got, ok := byCode[cur.Code]
		if !ok {
			t.Fatalf("byCode 缺少 %s", cur.Code)
		}
		if got != cur {
			t.Fatalf("byCode[%s] 与 table 不一致", cur.Code)
		}
	}
}

// TestConstantsMatchTable 币种代码常量与策展表**双向**一一对应。
//
// 双向都查是刻意的：只查「常量都在表内」会漏掉「表里有币种但没定义常量」
// （调用方只能写字符串字面量，失去编译期保护）；只查反向则会漏掉「定义了常量但表里没有」
// （`currency.XXX` 编译通过但运行时 Get 返回 false）。
func TestConstantsMatchTable(t *testing.T) {
	constants := []Code{
		CNY, USD, EUR, GBP, CHF,
		HKD, MOP, TWD, SGD, AUD, NZD, CAD, THB, MYR, PHP, IDR, INR,
		RUB, SEK, NOK, DKK, TRY, ZAR, BRL, AED, SAR,
		JPY, KRW, VND,
		KWD, BHD, OMR, JOD,
	}
	// 正向：每个常量都在表内
	for _, code := range constants {
		if !Valid(code) {
			t.Fatalf("常量 %s 不在策展表内", code)
		}
	}
	// 反向：表内每个币种都有对应常量
	declared := map[Code]bool{}
	for _, code := range constants {
		declared[code] = true
	}
	for _, cur := range table {
		if !declared[cur.Code] {
			t.Fatalf("策展表内的 %s 没有对应的导出常量", cur.Code)
		}
	}
	if len(constants) != len(table) {
		t.Fatalf("常量数与表项数不符: %d vs %d", len(constants), len(table))
	}
}

// TestGetAndScale Get / Scale 的命中与未命中分支。
func TestGetAndScale(t *testing.T) {
	cur, ok := Get(CNY)
	if !ok {
		t.Fatal("CNY 应在表内")
	}
	if cur.Code != CNY || cur.Name != "人民币" || cur.Scale != 2 || !cur.Enabled {
		t.Fatalf("CNY 枚举项不符: %+v", cur)
	}

	// 未命中：Get 返回零值 + false，Scale 返回 0 + false
	if _, ok := Get(Code("ZZZ")); ok {
		t.Fatal("ZZZ 不应在表内")
	}
	if scale, ok := Scale(Code("ZZZ")); ok || scale != 0 {
		t.Fatalf("未知币种的 Scale 应为 (0,false)，实际 (%d,%v)", scale, ok)
	}
	// 命中
	if scale, ok := Scale(JPY); !ok || scale != 0 {
		t.Fatalf("JPY 小数位数应为 (0,true)，实际 (%d,%v)", scale, ok)
	}
	if scale, ok := Scale(KWD); !ok || scale != 3 {
		t.Fatalf("KWD 小数位数应为 (3,true)，实际 (%d,%v)", scale, ok)
	}

	// 零值不是币种
	if Valid(Code("")) {
		t.Fatal("零值不应判为合法币种")
	}
	if _, ok := Scale(Code("")); ok {
		t.Fatal("零值不应查到小数位数")
	}
}

// TestCaseSensitivity 大小写敏感、不 trim（清洗归 parser，见包注释）。
func TestCaseSensitivity(t *testing.T) {
	for _, bad := range []Code{"cny", "Cny", "cNy", " CNY", "CNY ", "CNY\n", "\tCNY"} {
		if Valid(bad) {
			t.Fatalf("%q 不应判为合法（本包不做大小写归一与 trim）", bad)
		}
		if err := Validate(bad); err == nil {
			t.Fatalf("%q 应校验失败", bad)
		}
	}
}

// TestValidVsEnabled Valid 与 Enabled 的分工：Valid 含停用项，Enabled 不含。
//
// 本轮策展表全部启用，故用一个**临时注入的停用项**来验证这条分工——
// 这正是「只能停用不能删除」纪律生效后必然出现的状态，必须现在就把行为钉死，
// 否则将来第一次停用某币种时才发现历史明细读不出来。
func TestValidVsEnabled(t *testing.T) {
	const legacy Code = "ZWL" // 借一个真实的已退出流通代码作停用样本
	if Valid(legacy) {
		t.Fatalf("前置条件变了：%s 已被收录进策展表，请换一个未收录的代码做本用例", legacy)
	}

	// 临时注入停用项（用完即恢复，避免污染其他用例）
	byCode[legacy] = Currency{
		Code: legacy, Name: "津巴布韦元（已停用）", Symbol: "ZWL",
		Scale: 2, Enabled: false, FxSource: FxSourceDefault,
	}
	defer delete(byCode, legacy)

	// 读侧：合法（历史明细要能读回、展示、参与 I-7 重算）
	if !Valid(legacy) {
		t.Fatal("停用币种应仍判为合法（只能停用不能删除）")
	}
	if err := Validate(legacy); err != nil {
		t.Fatalf("停用币种的读侧校验应通过: %+v", err)
	}
	// 小数位数仍可查——这是历史摊分能重算的前提
	if scale, ok := Scale(legacy); !ok || scale != 2 {
		t.Fatalf("停用币种仍应可查小数位数，实际 (%d,%v)", scale, ok)
	}
	// JSON 与库读回都要收（读链路）
	var c Code
	if err := c.UnmarshalText([]byte(legacy)); err != nil {
		t.Fatalf("停用币种应能从 JSON 读回: %+v", err)
	}
	var c2 Code
	if err := c2.Scan(string(legacy)); err != nil {
		t.Fatalf("停用币种应能从库读回: %+v", err)
	}

	// 写侧：不可选
	if Enabled(legacy) {
		t.Fatal("停用币种不应判为已启用")
	}
	if err := ValidateEnabled(legacy); err == nil {
		t.Fatal("停用币种的写侧校验应失败")
	}
	// 停用币种也不能作本位币（T38 要求「已启用」）
	if CanBeBase(legacy) {
		t.Fatal("停用币种不应可作本位币")
	}
	if err := ValidateBase(legacy); err == nil {
		t.Fatal("停用币种作本位币应失败")
	}
	// 且档位与文案键应为「非法」而非「不可作本位币」（不泄露停用这一内部状态）
	var e *errs.Error
	if err := ValidateBase(legacy); !asErrsError(err, &e) {
		t.Fatalf("应返回 *errs.Error，实际 %T", err)
	} else if e.Key() != errs.KeyCurrencyInvalid {
		t.Fatalf("停用币种作本位币的文案键应为 %s，实际 %s", errs.KeyCurrencyInvalid, e.Key())
	}

	// 集合类接口：All 含停用项，EnabledAll 与 BaseCandidates 不含
	if !containsCode(allFromMap(), legacy) {
		t.Fatal("byCode 应含停用项")
	}
	if containsCode(EnabledAll(), legacy) {
		t.Fatal("EnabledAll 不应含停用项")
	}
	if containsCode(BaseCandidates(), legacy) {
		t.Fatal("BaseCandidates 不应含停用项")
	}
}

// TestT38BaseCandidates T38 口径：可作本位币 = 已启用 且 小数位数 ≤ 2。
func TestT38BaseCandidates(t *testing.T) {
	// 2 位与 0 位可作本位币
	for _, code := range []Code{CNY, USD, EUR, HKD, JPY, KRW, VND} {
		if !CanBeBase(code) {
			t.Fatalf("%s（小数位数 ≤ 2 且启用）应可作本位币", code)
		}
		if err := ValidateBase(code); err != nil {
			t.Fatalf("%s 作本位币校验应通过: %+v", code, err)
		}
	}
	// 3 位不可作本位币，但**可以**作支出币种
	for _, code := range []Code{KWD, BHD, OMR, JOD} {
		if CanBeBase(code) {
			t.Fatalf("%s（3 位小数）不应可作本位币", code)
		}
		// 关键分工：支出币种不受 T38 限制
		if !Enabled(code) {
			t.Fatalf("%s 应可正常作支出币种（T38 只约束本位币）", code)
		}
		if err := ValidateEnabled(code); err != nil {
			t.Fatalf("%s 作支出币种应通过: %+v", code, err)
		}
	}

	// BaseCandidates 与逐项 CanBeBase 必须完全一致
	got := BaseCandidates()
	inCandidates := map[Code]bool{}
	for _, cur := range got {
		inCandidates[cur.Code] = true
		if cur.Scale > baseMaxScale {
			t.Fatalf("候选集含 %s，其小数位数 %d > %d", cur.Code, cur.Scale, baseMaxScale)
		}
		if !cur.Enabled {
			t.Fatalf("候选集含未启用的 %s", cur.Code)
		}
	}
	for _, cur := range table {
		if want := cur.SupportsBase(); want != inCandidates[cur.Code] {
			t.Fatalf("%s：SupportsBase=%v，在候选集内=%v", cur.Code, want, inCandidates[cur.Code])
		}
	}

	// CNY 必须在候选集内（I-2 默认本位币）
	if !inCandidates[CNY] {
		t.Fatal("CNY 必须可作本位币（I-2 默认本位币）")
	}
}

// TestValidateBaseErrorKinds ValidateBase 的两档错误必须可区分——
// 这是 KeyCurrencyNotBaseCandidate 存在的全部意义。
func TestValidateBaseErrorKinds(t *testing.T) {
	// 未知代码 → currency.code.invalid
	var e1 *errs.Error
	err := ValidateBase(Code("ZZZ"))
	if !asErrsError(err, &e1) {
		t.Fatalf("应返回 *errs.Error，实际 %T", err)
	}
	if e1.Key() != errs.KeyCurrencyInvalid {
		t.Fatalf("未知代码的文案键应为 %s，实际 %s", errs.KeyCurrencyInvalid, e1.Key())
	}
	if e1.Kind() != errs.KindInput {
		t.Fatalf("档位应为 KindInput，实际 %v", e1.Kind())
	}

	// 合法但 3 位 → currency.code.not_base_candidate
	var e2 *errs.Error
	err = ValidateBase(KWD)
	if !asErrsError(err, &e2) {
		t.Fatalf("应返回 *errs.Error，实际 %T", err)
	}
	if e2.Key() != errs.KeyCurrencyNotBaseCandidate {
		t.Fatalf("3 位币种的文案键应为 %s，实际 %s", errs.KeyCurrencyNotBaseCandidate, e2.Key())
	}
	if e2.Kind() != errs.KindInput {
		t.Fatalf("档位应为 KindInput，实际 %v", e2.Kind())
	}
	// 两档必须不同，否则用户无从区分「币种不存在」与「不能当本位币」
	if e1.Key() == e2.Key() {
		t.Fatal("两档错误的文案键不应相同")
	}

	// 占位参数必须带上币种代码，否则文案渲染不出「哪个币种」
	args := e2.Args()
	if got, ok := args[errs.ArgCurrency]; !ok || got != KWD.String() {
		t.Fatalf("错误应带 %s=%s 占位参数，实际 %+v", errs.ArgCurrency, KWD, args)
	}
}

// TestValidateErrorContract Validate / ValidateEnabled 的错误档位与文案键。
func TestValidateErrorContract(t *testing.T) {
	for _, fn := range []struct {
		name string
		call func(Code) error
	}{
		{"Validate", Validate},
		{"ValidateEnabled", ValidateEnabled},
	} {
		t.Run(fn.name, func(t *testing.T) {
			if err := fn.call(CNY); err != nil {
				t.Fatalf("CNY 应通过: %+v", err)
			}
			var e *errs.Error
			err := fn.call(Code("ZZZ"))
			if !asErrsError(err, &e) {
				t.Fatalf("应返回 *errs.Error，实际 %T", err)
			}
			if e.Kind() != errs.KindInput {
				t.Fatalf("档位应为 KindInput，实际 %v", e.Kind())
			}
			if e.Key() != errs.KeyCurrencyInvalid {
				t.Fatalf("文案键应为 %s，实际 %s", errs.KeyCurrencyInvalid, e.Key())
			}
			// 零值也应失败（零值是「未设置」，不是币种）
			if err := fn.call(Code("")); err == nil {
				t.Fatal("零值应校验失败")
			}
		})
	}
}

// TestAllOrderAndCopy All / EnabledAll / BaseCandidates 的顺序稳定性与副本语义。
func TestAllOrderAndCopy(t *testing.T) {
	// 顺序 = 策展表声明顺序，且首位是 CNY（I-2 默认本位币，K-1 下拉首选）
	all := All()
	if len(all) != len(table) {
		t.Fatalf("All 数量不符: %d vs %d", len(all), len(table))
	}
	for i := range table {
		if all[i] != table[i] {
			t.Fatalf("第 %d 项顺序不符: %s vs %s", i, all[i].Code, table[i].Code)
		}
	}
	if all[0].Code != CNY {
		t.Fatalf("首位应为 CNY，实际 %s", all[0].Code)
	}

	// 多次调用顺序稳定（若内部按 map 迭代，下拉项会每次跳动）
	for i := 0; i < 5; i++ {
		again := All()
		for j := range again {
			if again[j] != all[j] {
				t.Fatalf("第 %d 次调用顺序不稳定", i)
			}
		}
	}

	// 副本语义：改返回值不影响策展表
	all[0].Name = "被改坏了"
	all[0].Scale = 99
	if cur := MustGet(CNY); cur.Name == "被改坏了" || cur.Scale == 99 {
		t.Fatal("All 应返回副本，调用方修改不得影响策展表")
	}

	// EnabledAll / BaseCandidates 是 All 的子集且保持相对顺序
	for _, sub := range [][]Currency{EnabledAll(), BaseCandidates()} {
		prev := -1
		for _, cur := range sub {
			idx := indexInTable(cur.Code)
			if idx < 0 {
				t.Fatalf("%s 不在策展表内", cur.Code)
			}
			if idx <= prev {
				t.Fatalf("相对顺序被打乱: %s", cur.Code)
			}
			prev = idx
		}
	}
}

// TestJSONContract JSON 序列化契约（T34② 前端契约侧）。
func TestJSONContract(t *testing.T) {
	// 序列化为字符串
	type resp struct {
		ExpenseCurrency Code `json:"expenseCurrency"`
		CardCurrency    Code `json:"cardCurrency"`
	}
	raw, err := json.Marshal(resp{ExpenseCurrency: CNY, CardCurrency: ""})
	if err != nil {
		t.Fatalf("序列化失败: %+v", err)
	}
	// 零值（卡片币种留空）序列化为空串而非报错
	const want = `{"expenseCurrency":"CNY","cardCurrency":""}`
	if string(raw) != want {
		t.Fatalf("JSON 契约不符:\n期望 %s\n实际 %s", want, raw)
	}

	// 反序列化：合法值
	t.Run("合法值", func(t *testing.T) {
		var r resp
		if err := json.Unmarshal([]byte(`{"expenseCurrency":"JPY","cardCurrency":"USD"}`), &r); err != nil {
			t.Fatalf("反序列化失败: %+v", err)
		}
		if r.ExpenseCurrency != JPY || r.CardCurrency != USD {
			t.Fatalf("值不符: %+v", r)
		}
	})

	// 反序列化：空串接受（= 卡片币种留空）
	t.Run("空串即留空", func(t *testing.T) {
		var r resp
		if err := json.Unmarshal([]byte(`{"expenseCurrency":"CNY","cardCurrency":""}`), &r); err != nil {
			t.Fatalf("空串应被接受: %+v", err)
		}
		if !r.CardCurrency.IsZero() {
			t.Fatal("空串应映射为零值")
		}
	})

	// 反序列化：非法值一律拒收
	t.Run("非法值拒收", func(t *testing.T) {
		for _, body := range []string{
			`{"expenseCurrency":"ZZZ"}`,  // 不存在
			`{"expenseCurrency":"cny"}`,  // 小写
			`{"expenseCurrency":"CN"}`,   // 两位
			`{"expenseCurrency":"CNYY"}`, // 四位
			`{"expenseCurrency":"XXX"}`,  // ISO「无币种」码，未收录
			`{"expenseCurrency":"BTC"}`,  // 非 ISO 4217
			`{"expenseCurrency":"CNH"}`,  // 非 ISO 4217（归一责任在 parser）
			`{"expenseCurrency":" CNY"}`, // 带空白
			`{"expenseCurrency":"XAU"}`,  // 贵金属，非流通货币
			`{"expenseCurrency":"CLF"}`,  // 4 位记账单位，刻意未收录
		} {
			var r resp
			if err := json.Unmarshal([]byte(body), &r); err == nil {
				t.Fatalf("非法请求 %s 应被拒收", body)
			}
		}
	})
}

// TestUnmarshalTextErrorContract UnmarshalText 的错误档位（应为用户输入错误）。
func TestUnmarshalTextErrorContract(t *testing.T) {
	var c Code
	err := c.UnmarshalText([]byte("ZZZ"))
	var e *errs.Error
	if !asErrsError(err, &e) {
		t.Fatalf("应返回 *errs.Error，实际 %T", err)
	}
	if e.Kind() != errs.KindInput {
		t.Fatalf("JSON 反序列化失败应为 KindInput（用户输入错误），实际 %v", e.Kind())
	}
	if e.Key() != errs.KeyCurrencyInvalid {
		t.Fatalf("文案键应为 %s，实际 %s", errs.KeyCurrencyInvalid, e.Key())
	}
}

// TestFxSource 汇率源标识：本轮只有默认源，且策展表内每一项都引用已收录的源。
func TestFxSource(t *testing.T) {
	if !FxSourceDefault.Valid() {
		t.Fatal("默认汇率源应为已收录")
	}
	for _, bad := range []FxSource{"", "ecb", "boc", "DEFAULT"} {
		if bad.Valid() {
			t.Fatalf("%q 不应判为已收录的汇率源", bad)
		}
	}
	// 每个币种都要有可路由的汇率源——否则 fxrate 注册表会路由到无人注册的源
	for _, cur := range table {
		if !cur.FxSource.Valid() {
			t.Fatalf("%s 的汇率源 %q 未收录", cur.Code, cur.FxSource)
		}
	}
}

// TestMustGetPanics MustGet 对未收录代码必须 panic（它只许用于包内常量与测试）。
func TestMustGetPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustGet 对未收录代码应 panic")
		}
	}()
	MustGet(Code("ZZZ"))
}

// TestScaleCoversAllBranches 策展表必须覆盖 0 / 2 / 3 三类小数位数分支，
// 否则 8.2 摊分与 1a 折算的边界在真实数据上永远走不到。
func TestScaleCoversAllBranches(t *testing.T) {
	dist := map[int32]int{}
	for _, cur := range table {
		dist[cur.Scale]++
	}
	for _, scale := range []int32{0, 2, 3} {
		if dist[scale] == 0 {
			t.Fatalf("策展表未覆盖 %d 位小数的币种", scale)
		}
	}
	// 3 位币种必须存在且全部落在本位币候选集之外——这是 T38 分支的活体样本
	if dist[3] == 0 {
		t.Fatal("需保留 3 位小数币种作为 T38「不可作本位币」分支的样本")
	}
}

// ---------- 测试辅助 ----------

// asErrsError 把 error 断言为 *errs.Error。
func asErrsError(err error, target **errs.Error) bool {
	if err == nil {
		return false
	}
	e, ok := err.(*errs.Error)
	if ok {
		*target = e
	}
	return ok
}

// allFromMap 从 byCode 取全部项（含临时注入的停用项），供停用分支用例使用。
func allFromMap() []Currency {
	out := make([]Currency, 0, len(byCode))
	for _, cur := range byCode {
		out = append(out, cur)
	}
	return out
}

// containsCode 判断切片中是否含某代码。
func containsCode(list []Currency, code Code) bool {
	for _, cur := range list {
		if cur.Code == code {
			return true
		}
	}
	return false
}

// indexInTable 返回某代码在策展表中的下标，不存在返回 -1。
func indexInTable(code Code) int {
	for i, cur := range table {
		if cur.Code == code {
			return i
		}
	}
	return -1
}
