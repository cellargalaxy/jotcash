package decimal

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDecimalIsNotComparable 是本包最重要的一条可执行断言（方案取舍 1）。
//
// 第三方库的 Decimal 是 comparable，而 == 比的是内部表示不是数值：
// 1.50 == 1.5 得 false 且**编译通过、不报错**。8.3 判重的四要素含支出金额，
// 一旦写成 == 就静默漏判重复。
//
// 本测试断言本包的 Decimal 在类型系统层面**不可比较**，从而使 ==、
// 含金额字段的结构体整体比较、map[Decimal]T 全部成为编译错误。
// 靠人工评审守不住这条，必须由机器判定。
func TestDecimalIsNotComparable(t *testing.T) {
	//核心断言：Decimal 必须不可比较，否则 == 会静默算错金额相等
	if reflect.TypeOf(Decimal{}).Comparable() {
		t.Error("Decimal 竟然是可比较类型：== 将编译通过但比较内部表示而非数值，" +
			"1.50 == 1.5 会得到 false，8.3 四要素判重会静默漏判重复。" +
			"必须保留 noCmp 内嵌字段。")
	}

	//含 Decimal 字段的结构体也必须不可比较，即实体整体 == 同样被拦下。
	//这一条覆盖的是 entity.Expense 这类真实用法。
	type entityLike struct {
		Amount Decimal
		Name   string
	}
	if reflect.TypeOf(entityLike{}).Comparable() {
		t.Error("含 Decimal 字段的结构体竟然可比较：实体整体 == 会静默算错")
	}

	//作为对照：确认这套判定确实能区分可比较类型，避免断言恒真而失去意义
	type plain struct{ Name string }
	if !reflect.TypeOf(plain{}).Comparable() {
		t.Error("对照用的普通结构体应当可比较，说明本判定方式有误")
	}

	//不可比较也意味着不能作 map 键。这里用 reflect 构造 map 类型来验证：
	//若 Decimal 可比较，MapOf 会成功，说明防线失效。
	func() {
		defer func() {
			if recover() == nil {
				t.Error("map[Decimal]T 竟然可以构造：Decimal 应不可作 map 键")
			}
		}()
		_ = reflect.MapOf(reflect.TypeOf(Decimal{}), reflect.TypeOf(""))
	}()
}

// TestNoFloatInPublicAPI 断言本包不提供任何 float 出入口（方案取舍 2）。
//
// 承载写的是「金额一律不用浮点」。若提供 float 构造或输出，这句话就只是约定；
// 不提供，则「不用浮点」是编译期不可达的。本测试扫描全部导出标识符的签名。
func TestNoFloatInPublicAPI(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("扫描源文件报错: %v", err)
	}

	var offenders []string
	for _, name := range files {
		//只查生产代码，测试文件不构成对外 API
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读取 %s 报错: %v", name, err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("解析 %s 报错: %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				return true
			}
			//收集参数与返回值中出现的类型名
			check := func(fields *ast.FieldList, kind string) {
				if fields == nil {
					return
				}
				for _, f := range fields.List {
					if ident, ok := f.Type.(*ast.Ident); ok {
						if ident.Name == "float32" || ident.Name == "float64" {
							offenders = append(offenders,
								fn.Name.Name+" 的"+kind+"含 "+ident.Name)
						}
					}
				}
			}
			check(fn.Type.Params, "参数")
			check(fn.Type.Results, "返回值")
			return true
		})
	}

	if len(offenders) > 0 {
		t.Errorf("本包导出 API 出现浮点类型，违反「金额一律不用浮点」：%v", offenders)
	}
}

// TestDangerousAPIsNotExposed 断言三个危险 API 未被暴露（方案取舍 5、包注释）。
//
//  1. 银行家舍入：把 5.45 舍成 5.4，与已定案的四舍五入口径不符；
//  2. 除法：底层库的 Div 受包级可变量控制精度，任何包都能远程篡改金额除法；
//  3. 取整数部分：对超出 int64 的值静默返回错误整数。
func TestDangerousAPIsNotExposed(t *testing.T) {
	//收集 Decimal 的全部导出方法名。
	//reflect 只能看到导出方法，正好就是对外 API 面。
	typ := reflect.TypeOf(Decimal{})
	methods := make(map[string]bool, typ.NumMethod())
	for i := 0; i < typ.NumMethod(); i++ {
		methods[typ.Method(i).Name] = true
	}
	//指针接收者的方法（UnmarshalJSON）挂在指针类型上
	ptrType := reflect.TypeOf(&Decimal{})
	for i := 0; i < ptrType.NumMethod(); i++ {
		methods[ptrType.Method(i).Name] = true
	}

	//这些名字一旦出现，说明危险语义被引入
	banned := map[string]string{
		"RoundBank":      "银行家舍入会把 5.45 舍成 5.4，与已定案的四舍五入口径不符",
		"RoundCash":      "现金舍入非本系统口径",
		"RoundUp":        "朝无穷方向舍入无消费方，且与四舍五入口径冲突",
		"RoundDown":      "朝无穷方向舍入无消费方",
		"RoundCeil":      "朝无穷方向舍入无消费方",
		"RoundFloor":     "朝无穷方向舍入无消费方",
		"Div":            "底层 Div 受包级可变量控制精度，任何包都能远程篡改金额除法",
		"DivRound":       "同上，且会隐藏位数决策",
		"IntPart":        "对超出 int64 的值静默返回截断后的错误整数",
		"Float64":        "浮点出口违反「金额一律不用浮点」",
		"InexactFloat64": "浮点出口违反「金额一律不用浮点」",
		"Value":          "落库位数由列口径决定，隐式 Value 必须替 store 猜位数，猜错即静默改钱",
		"Scan":           "同上，回读位数同样不应由本包猜定",
	}
	for name, why := range banned {
		if methods[name] {
			t.Errorf("危险 API %s 被暴露：%s", name, why)
		}
	}

	//同时确认必要 API 确实存在（防止「因为全删了所以通过」这种假通过）
	required := []string{
		"Round", "RoundRate", "QuoRem", "Equal", "Cmp", "Units",
		"StringFixed", "FitsScale", "Scale", "String", "Add", "Sub",
		"Mul", "Neg", "Abs", "IsZero", "IsNegative", "IsPositive",
		"Sign", "MarshalJSON", "UnmarshalJSON",
	}
	for _, name := range required {
		if !methods[name] {
			t.Errorf("必要 API %s 缺失", name)
		}
	}
}

// TestConsumerConvertRule 走查 rule/convert 的用法（不变式 1a 的唯一实现处）。
//
// 折算链路：原币金额 × 规整后汇率 → 按本位币小数位数舍入 → 按 2 位定点存储。
func TestConsumerConvertRule(t *testing.T) {
	//模拟 rule/convert：位数由 currency 提供，本包只接收参数
	convert := func(amount, rawRate Decimal, baseScale int32) (converted, rate Decimal, err error) {
		//T39②：汇率在唯一规整点规整到 8 位，自动获取与手填共用
		rate = rawRate.RoundRate()
		//乘积不中途舍入，只在最后按本位币位数舍入一次
		converted, err = amount.Mul(rate).Round(baseScale, RoundHalfUp)
		return converted, rate, err
	}

	cases := []struct {
		amount    string
		rawRate   string
		baseScale int32
		wantRate  string
		wantAmt   string
		desc      string
	}{
		{"100.00", "7.1234567891", 2, "7.12345679", "712.35", "本位币 2 位，汇率截到 8 位"},
		{"100.00", "7.1234567891", 0, "7.12345679", "712", "本位币 0 位"},
		{"100.00", "7.1234567891", 1, "7.12345679", "712.3", "本位币 1 位"},
		{"-100.00", "7.1234567891", 2, "7.12345679", "-712.35", "负金额（退款）"},
		{"0", "7.12345679", 2, "7.12345679", "0", "零金额"},
		{"100.00", "1", 2, "1", "100", "汇率为 1（本位币本身）"},
	}
	for _, c := range cases {
		converted, rate, err := convert(MustParse(c.amount), MustParse(c.rawRate), c.baseScale)
		if err != nil {
			t.Errorf("[%s] 报错: %v", c.desc, err)
			continue
		}
		if rate.String() != c.wantRate {
			t.Errorf("[%s] 规整后汇率 = %s, 期望 %s", c.desc, rate.String(), c.wantRate)
		}
		if converted.String() != c.wantAmt {
			t.Errorf("[%s] 折算金额 = %s, 期望 %s", c.desc, converted.String(), c.wantAmt)
		}
		//汇率必须能落 8 位列
		if !rate.FitsScale(RateScale) {
			t.Errorf("[%s] 汇率 %s 无法落 %d 位列", c.desc, rate.String(), RateScale)
		}
		//本位币金额必须能落 2 位定点列（T38 无损性前提）
		if !converted.FitsScale(BaseAmountStoreScale) {
			t.Errorf("[%s] 本位币金额 %s 无法落 %d 位定点列",
				c.desc, converted.String(), BaseAmountStoreScale)
		}
		if _, err := converted.Units(BaseAmountStoreScale); err != nil {
			t.Errorf("[%s] 本位币金额 %s 落库报错: %v", c.desc, converted.String(), err)
		}
	}
}

// TestConsumerDedupFourFactors 走查 rule/dedup 的 8.3 四要素判重。
//
// 四要素为「归属用户 + 支出日期 + 支出金额 + 支出币种」等值匹配，不存指纹字段。
// 其中支出金额的相等判定必须走 Equal——这是 Decimal 设计为不可比较的原因：
// 1.50 与 1.5 是同一笔金额，用 == 会判为不等而漏判重复。
func TestConsumerDedupFourFactors(t *testing.T) {
	//模拟四要素（除金额外三项用占位类型，真实类型由 entity 与 calendar 决定）
	type txn struct {
		userID   int64   //归属用户
		date     string  //支出日期
		amount   Decimal //支出金额
		currency string  //支出币种
	}
	//四要素等值判定：金额必须用 Equal
	same := func(a, b txn) bool {
		return a.userID == b.userID &&
			a.date == b.date &&
			a.amount.Equal(b.amount) && //关键：不能用 ==
			a.currency == b.currency
	}

	base := txn{userID: 1001, date: "2026-01-15", amount: MustParse("100.50"), currency: "CNY"}

	//同一笔金额的不同文本写法必须判为重复
	dupCases := []struct {
		amount string
		desc   string
	}{
		{"100.50", "完全相同"},
		{"100.5", "省略尾随零"},
		{"100.500", "多写尾随零"},
		{"+100.50", "带正号"},
		{"1.005e2", "科学计数法"},
	}
	for _, c := range dupCases {
		other := base
		other.amount = MustParse(c.amount)
		if !same(base, other) {
			t.Errorf("[%s] %s 与 100.50 应判为同一笔金额（重复）", c.desc, c.amount)
		}
	}

	//金额不同则不是重复
	diff := base
	diff.amount = MustParse("100.51")
	if same(base, diff) {
		t.Error("100.51 与 100.50 不应判为重复")
	}
	//归属用户不同不是重复（跨用户不比对）
	otherUser := base
	otherUser.userID = 1002
	if same(base, otherUser) {
		t.Error("归属用户不同不应判为重复")
	}
	//其余要素不同同样不是重复
	otherDate := base
	otherDate.date = "2026-01-16"
	if same(base, otherDate) {
		t.Error("日期不同不应判为重复")
	}

	//归一化的 map 键形态（供批量判重时建索引）：
	//同一金额的不同写法必须得到同一个键
	//
	//键必须用 String() 而非 StringFixed(BaseAmountStoreScale)：
	//8.3 的「支出金额」是**原币**金额，小数位数随支出币种而定——KWD 是 3 位，
	//而 BaseAmountStoreScale 是**本位币**的存储位数（2 位）。用后者建键时，
	//KWD 记的账会直接撞上 ErrPrecisionLoss（「值 0.001 需 3 位, 目标 2 位」），
	//judgement 键根本生成不出来。String() 已剥掉尾随零，对任意位数都成立。
	key := func(amount Decimal) string {
		return amount.String()
	}
	if key(MustParse("100.50")) != key(MustParse("100.5")) {
		t.Error("100.50 与 100.5 应生成同一个判重键")
	}
	if key(MustParse("-0")) != key(MustParse("0")) {
		t.Error("-0 与 0 应生成同一个判重键")
	}
	//3 位小数的原币金额（KWD 这类币种）必须能建键，且同值不同写法同键
	if key(MustParse("0.001")) != key(MustParse("0.00100")) {
		t.Error("0.001 与 0.00100 应生成同一个判重键")
	}
	//反向确认上面那段理由属实：按本位币 2 位定点建键时，3 位小数金额会失败
	if _, err := MustParse("0.001").StringFixed(BaseAmountStoreScale); err == nil {
		t.Error("前提已变：3 位小数金额竟能被 2 位定点无损容纳，" +
			"若如此则判重键的位数口径需重新评估")
	}
	//键必须与 Equal 同进同退——这是下游 rule/dedup 用 map 索引判重的前提
	//（那里有穷举交叉验证，此处只钉住本包这一侧的契约）
	for _, pair := range [][2]string{
		{"100.50", "100.5"}, {"0", "-0"}, {"1e3", "1000"}, {"0.001", "0.00100"},
	} {
		a, b := MustParse(pair[0]), MustParse(pair[1])
		if a.Equal(b) != (key(a) == key(b)) {
			t.Errorf("%s 与 %s: Equal=%t 但键相等=%t，判重键与 Equal 必须同进同退",
				pair[0], pair[1], a.Equal(b), key(a) == key(b))
		}
	}
}

// TestConsumerStoreColumnForms 走查 store/sqlite 的两种列形态。
//
// 本包不实现 driver.Valuer / sql.Scanner（理由见包注释），
// 要求 store 显式调 Units 或 StringFixed 指定位数。这里验证两种形态都可无损往返。
func TestConsumerStoreColumnForms(t *testing.T) {
	samples := []struct {
		value string
		scale int32
		desc  string
	}{
		{"100.50", BaseAmountStoreScale, "金额列 2 位"},
		{"0", BaseAmountStoreScale, "零金额"},
		{"-33.34", BaseAmountStoreScale, "负金额"},
		{"7.12345678", RateScale, "汇率列 8 位"},
		{"1", RateScale, "汇率为 1"},
		{strings.Repeat("9", MaxIntDigits), BaseAmountStoreScale, "整数位上界"},
	}
	for _, s := range samples {
		d := MustParse(s.value)

		//形态一：整数最小单位
		units, err := d.Units(s.scale)
		if err != nil {
			t.Errorf("[%s] Units 报错: %v", s.desc, err)
			continue
		}
		back, err := FromUnits(units, s.scale)
		if err != nil {
			t.Errorf("[%s] FromUnits 报错: %v", s.desc, err)
			continue
		}
		if !back.Equal(d) {
			t.Errorf("[%s] 最小单位往返失真: %s -> %d -> %s", s.desc, s.value, units, back.String())
		}

		//形态二：定点文本
		text, err := d.StringFixed(s.scale)
		if err != nil {
			t.Errorf("[%s] StringFixed 报错: %v", s.desc, err)
			continue
		}
		parsed, err := Parse(text)
		if err != nil {
			t.Errorf("[%s] 定点文本回读报错: %v", s.desc, err)
			continue
		}
		if !parsed.Equal(d) {
			t.Errorf("[%s] 定点文本往返失真: %s -> %s -> %s", s.desc, s.value, text, parsed.String())
		}
	}
}

// TestConsumerRangeFilter 走查 store 的 F-4 金额区间筛选。
//
// 区间比较必须走数值比较（Cmp），不能用文本比较——
// 文本序下「100.00」会小于「9.99」。
func TestConsumerRangeFilter(t *testing.T) {
	//模拟 store 的筛选判定：零值端点视为无约束
	//（与 calendar 的日期区间筛选同一取向）
	match := func(amount, from, to Decimal, hasFrom, hasTo bool) bool {
		if hasFrom && amount.Cmp(from) < 0 {
			return false
		}
		if hasTo && amount.Cmp(to) > 0 {
			return false
		}
		return true
	}

	amount := MustParse("50.00")
	if !match(amount, Decimal{}, Decimal{}, false, false) {
		t.Error("两端未指定时应全部命中")
	}
	if !match(amount, MustParse("10.00"), Decimal{}, true, false) {
		t.Error("只给下界且满足时应命中")
	}
	if match(amount, MustParse("100.00"), Decimal{}, true, false) {
		t.Error("只给下界且低于下界时应落空")
	}
	if !match(amount, Decimal{}, MustParse("100.00"), false, true) {
		t.Error("只给上界且满足时应命中")
	}
	if match(amount, Decimal{}, MustParse("10.00"), false, true) {
		t.Error("只给上界且高于上界时应落空")
	}
	//闭区间：端点应命中
	if !match(amount, MustParse("50.00"), MustParse("50.00"), true, true) {
		t.Error("上下界均为该值时应命中（闭区间）")
	}
	//不同文本写法的同一数值必须同结论
	if !match(amount, MustParse("50.0"), MustParse("50.000"), true, true) {
		t.Error("端点用不同写法表示同一数值时应命中")
	}

	//反面对照：文本比较会给出错误结论
	small, large := MustParse("9.99"), MustParse("100.00")
	if small.Cmp(large) >= 0 {
		t.Error("9.99 应小于 100.00")
	}
	if small.String() < large.String() {
		t.Log("注意：文本比较下 \"9.99\" < \"100\" 不成立，这正是必须用 Cmp 的原因")
	}
}

// TestConsumerParserSentinelErrors 走查 parser 的错误分类映射。
//
// 本包不依赖 base/errs（L0 层内零依赖），只给可 errors.Is 判定的哨兵错误，
// 文案键与错误档位由 parser 附加（D-3 五类解析错误）。
func TestConsumerParserSentinelErrors(t *testing.T) {
	//模拟 parser 拿到账单里的金额文本后的分类处理
	classify := func(raw string) string {
		_, err := Parse(raw)
		switch {
		case err == nil:
			return "ok"
		case errors.Is(err, ErrSyntax):
			return "格式不符" //D-3 五类错误之一
		case errors.Is(err, ErrIntDigitsExceeded), errors.Is(err, ErrScaleExceeded):
			return "数值超出范围" //D-3 五类错误之一
		default:
			return "其他"
		}
	}

	cases := []struct{ raw, want string }{
		{"100.50", "ok"},
		{"-33.34", "ok"},
		{"", "格式不符"},
		{"abc", "格式不符"},
		{"1,000.50", "格式不符"},
		{" 100.50", "格式不符"},
		{strings.Repeat("9", 30), "数值超出范围"},
		{"1e1000000", "数值超出范围"},
		{"1e-2000", "数值超出范围"},
	}
	for _, c := range cases {
		if got := classify(c.raw); got != c.want {
			t.Errorf("classify(%q) = %q, 期望 %q", c.raw, got, c.want)
		}
	}

	//落库阶段的两类错误同样可分类
	if _, err := MustParse("1.005").Units(BaseAmountStoreScale); !errors.Is(err, ErrPrecisionLoss) {
		t.Errorf("期望 ErrPrecisionLoss, 实际 %v", err)
	}
	if _, err := MustParse(strings.Repeat("9", MaxIntDigits)).Units(RateScale); !errors.Is(err, ErrUnitsOverflow) {
		t.Errorf("期望 ErrUnitsOverflow, 实际 %v", err)
	}
}

// TestConsumerStatAggregation 走查 service/stat 的 F-6 合计与 J 域月度聚合。
//
// 聚合是多笔累加，不设量级上界（入口已保证每一笔在界内），
// 否则会误拒合法统计。
func TestConsumerStatAggregation(t *testing.T) {
	//1 万笔金额累加，验证不丢精度也不误拒
	var total Decimal
	const count = 10000
	each := MustParse("99.99")
	for i := 0; i < count; i++ {
		total = total.Add(each)
	}
	want := MustParse("999900")
	if !total.Equal(want) {
		t.Errorf("1 万笔 99.99 累加 = %s, 期望 %s", total.String(), want.String())
	}
	//聚合结果虽超出单笔量级校验的语义，但不应被拒
	if total.Scale() > BaseAmountStoreScale {
		t.Errorf("聚合结果位数 = %d, 不应超过 %d", total.Scale(), BaseAmountStoreScale)
	}

	//正负混合（含退款）
	mixed := Sum(MustParse("100.00"), MustParse("-30.50"), MustParse("0.01"), MustParse("-0.01"))
	if !mixed.Equal(MustParse("69.5")) {
		t.Errorf("正负混合合计 = %s, 期望 69.5", mixed.String())
	}

	//Sum 与逐笔 Add 必须同结果
	list := []Decimal{MustParse("10.01"), MustParse("20.02"), MustParse("-5.03")}
	var byAdd Decimal
	for _, d := range list {
		byAdd = byAdd.Add(d)
	}
	if !Sum(list...).Equal(byAdd) {
		t.Error("Sum 与逐笔 Add 结果不一致")
	}
}
