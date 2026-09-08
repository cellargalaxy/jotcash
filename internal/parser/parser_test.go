package parser

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// stubParser 是测试用的最小 Parser 实现。
//
// 本包是端口包、不含解析实现，因此测试必须自带一个实现来验证接口与注册表
// ——这也顺带证明了「接口可被实现」这件事本身（一个签名写歪的接口，实现时
// 才会暴露）。
type stubParser struct {
	pt   enum.ParserType
	rows []RawRow
	err  error

	// gotInput 记录最后一次收到的入参，供「入参含文件内容与文件密码」的走查断言。
	gotInput Input
	// calls 记录调用次数，供快速失败语义的用例使用。
	calls int
}

func (p *stubParser) Type() enum.ParserType { return p.pt }

func (p *stubParser) Parse(ctx context.Context, in Input) ([]RawRow, error) {
	p.calls++
	p.gotInput = in
	if p.err != nil {
		return nil, p.err
	}
	return p.rows, nil
}

// 编译期断言：stubParser 满足 Parser。写在这里而不是靠 NewRegistry 的入参
// 隐式检查——后者若因签名变更而失败，报错会指向调用点而非实现处。
var _ Parser = (*stubParser)(nil)

// TestParserInterfaceShape 锁死 Parser 接口的形状：恰两个方法，签名如承载所述。
//
// 用反射逐项断言而不是只靠 stubParser 能编译：能编译只证明「存在一种实现」，
// 证明不了「接口没多出第三个方法」——而端口层多一个方法，所有 L4 实现都得跟着改。
func TestParserInterfaceShape(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf((*Parser)(nil)).Elem()
	if typ.Kind() != reflect.Interface {
		t.Fatalf("Parser 应为接口，实际 %v", typ.Kind())
	}
	if got := typ.NumMethod(); got != 2 {
		t.Errorf("Parser 应恰有 2 个方法（Type/Parse），实际 %d 个", got)
	}

	//Type() enum.ParserType
	m, ok := typ.MethodByName("Type")
	if !ok {
		t.Fatal("Parser 应有 Type 方法（解析器自报类型键，取舍 2）")
	}
	if m.Type.NumIn() != 0 || m.Type.NumOut() != 1 {
		t.Errorf("Type 应为 0 入 1 出，实际 %d 入 %d 出", m.Type.NumIn(), m.Type.NumOut())
	}
	if got := m.Type.Out(0); got != reflect.TypeOf(enum.ParserType("")) {
		t.Errorf("Type 应返回 enum.ParserType，实际 %v", got)
	}

	//Parse(context.Context, Input) ([]RawRow, error)
	m, ok = typ.MethodByName("Parse")
	if !ok {
		t.Fatal("Parser 应有 Parse 方法")
	}
	if m.Type.NumIn() != 2 || m.Type.NumOut() != 2 {
		t.Fatalf("Parse 应为 2 入 2 出，实际 %d 入 %d 出", m.Type.NumIn(), m.Type.NumOut())
	}
	if got := m.Type.In(0); got != reflect.TypeOf((*context.Context)(nil)).Elem() {
		t.Errorf("Parse 首参应为 context.Context（取舍 6），实际 %v", got)
	}
	if got := m.Type.In(1); got != reflect.TypeOf(Input{}) {
		t.Errorf("Parse 次参应为 Input，实际 %v", got)
	}
	if got := m.Type.Out(0); got != reflect.TypeOf([]RawRow{}) {
		t.Errorf("Parse 首个返回值应为 []RawRow，实际 %v", got)
	}
	if got := m.Type.Out(1); got != reflect.TypeOf((*error)(nil)).Elem() {
		t.Errorf("Parse 次个返回值应为 error，实际 %v", got)
	}
}

// TestInputCarriesContentAndPassword 走查承载「入参含文件内容与**文件密码**」
// （D-2）：两个字段都必须存在，且实现确实能读到调用方传的值。
//
// 这条是承载里被加粗点名的要求，且历史上极易漏——加密 PDF 的口令若没能传到
// 实现，症状是「密码填了也解不开」，而根因在端口结构体少一个字段。
func TestInputCarriesContentAndPassword(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(Input{})
	if got := typ.NumField(); got != 2 {
		t.Errorf("Input 应恰有 2 个字段（文件内容 + 文件密码），实际 %d 个", got)
	}
	content, ok := typ.FieldByName("Content")
	if !ok {
		t.Fatal("Input 应有 Content 字段（文件内容）")
	}
	if content.Type.Kind() != reflect.Slice || content.Type.Elem().Kind() != reflect.Uint8 {
		t.Errorf("Content 应为 []byte，实际 %v", content.Type)
	}
	password, ok := typ.FieldByName("Password")
	if !ok {
		t.Fatal("Input 应有 Password 字段（文件密码，D-2）")
	}
	if password.Type.Kind() != reflect.String {
		t.Errorf("Password 应为 string，实际 %v", password.Type)
	}

	//实际透传：实现读到的必须与传入的一致
	p := &stubParser{pt: enum.ParserTypeGenericCSV}
	want := Input{Content: []byte("date,amount\n2026-01-15,100.00\n"), Password: "s3cret"}
	if _, err := p.Parse(context.Background(), want); err != nil {
		t.Fatalf("Parse 意外失败: %v", err)
	}
	if string(p.gotInput.Content) != string(want.Content) {
		t.Errorf("实现收到的文件内容不一致: %q", p.gotInput.Content)
	}
	if p.gotInput.Password != want.Password {
		t.Errorf("实现收到的文件密码不一致: %q", p.gotInput.Password)
	}
}

// TestRawRowHasExactlyEightFields 锁死 RawRow 恰含承载点名的 8 个字段，
// 且四个刻意排除的派生字段（取舍 3）确实不在其中。
//
// 后半段是本用例的重点：多出「本位币金额」或「折算汇率」会直接违反前提 6 与
// I-5，而那种越界在代码评审里看起来只是「多了个字段，挺方便」。
func TestRawRowHasExactlyEightFields(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(RawRow{})

	wantFields := map[string]reflect.Type{
		"Date":         reflect.TypeOf(calendar.Date{}),
		"Amount":       reflect.TypeOf(decimal.Decimal{}),
		"Currency":     reflect.TypeOf(currency.Code{}),
		"Counterparty": reflect.TypeOf(""),
		"Remark":       reflect.TypeOf(""),
		"BankName":     reflect.TypeOf(""),
		"CardLast4":    reflect.TypeOf(""),
		"CardCurrency": reflect.TypeOf(currency.Code{}),
	}
	if got := typ.NumField(); got != len(wantFields) {
		t.Errorf("RawRow 应恰有 %d 个字段，实际 %d 个", len(wantFields), got)
	}
	for name, wantType := range wantFields {
		f, ok := typ.FieldByName(name)
		if !ok {
			t.Errorf("RawRow 缺少字段 %s（承载点名的 8 项之一）", name)
			continue
		}
		if f.Type != wantType {
			t.Errorf("RawRow.%s 类型应为 %v，实际 %v", name, wantType, f.Type)
		}
	}

	//四个刻意排除的字段：出现即违反取舍 3 所列的对应条款
	for _, forbidden := range []struct{ name, why string }{
		{"BaseAmount", "本位币金额只能来自 rule/convert，前提 6 无人工指定入口"},
		{"Rate", "折算汇率只能来自 service/fx，I-5 不静默按 1:1"},
		{"AmortizeMonths", "摊分月数来自用户输入默认 1（H-1），起止月由 rule/amortize 派生"},
		{"ExpenseType", "支出类型的自动打标随 T4 挂起，当前一律留空（G-3）"},
	} {
		if _, ok := typ.FieldByName(forbidden.name); ok {
			t.Errorf("RawRow 不应有 %s 字段：%s", forbidden.name, forbidden.why)
		}
	}
}

// TestRawRowZeroValueIsUsable 走查承载「解析不出则留空」：RawRow 零值可用，
// 8 个字段全部零值都是合法状态，且零值确实表达「未指定」而非某个真实值。
func TestRawRowZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var row RawRow

	if !row.Date.IsZero() {
		t.Error("零值 RawRow 的 Date 应为 IsZero（解析不出则留空）")
	}
	if !row.Currency.IsZero() {
		t.Error("零值 RawRow 的 Currency 应为 IsZero")
	}
	if !row.CardCurrency.IsZero() {
		t.Error("零值 RawRow 的 CardCurrency 应为 IsZero（卡片三属性可留空，D-2）")
	}
	if !row.Amount.IsZero() {
		t.Error("零值 RawRow 的 Amount 应为零")
	}
	for name, got := range map[string]string{
		"Counterparty": row.Counterparty,
		"Remark":       row.Remark,
		"BankName":     row.BankName,
		"CardLast4":    row.CardLast4,
	} {
		if got != "" {
			t.Errorf("零值 RawRow 的 %s 应为空串，实际 %q", name, got)
		}
	}

	//RawRow **不可比较**（== 不可用），因为 Amount 是 decimal.Decimal，
	//而后者刻意做成不可比较：1.50 与 1.5 数值相同而底层表示不同，== 会给出
	//「不相等」这个错误答案。判等必须逐字段来、金额用 Decimal.Equal。
	//
	//这条性质是 8.3 去重（rule/dedup）能正确工作的前提之一：那里的四要素
	//判重走的是 dedup 自己的键构造，而不是对结构体做 ==。
	if reflect.TypeOf(RawRow{}).Comparable() {
		t.Error("RawRow 不应可比较：Amount 为 decimal.Decimal，== 会把 1.50 与 1.5 判为不等")
	}
}

// TestRawRowHasNoMethods 锁死 RawRow 是**纯数据载体**：零方法。
//
// 与 entity 的 5 个实体同一取向。一旦有人加了 ToExpense()，本用例失败——
// 那个方法需要 import entity，而承载明写本包不依赖 entity（映射归
// service/intake）。
func TestRawRowHasNoMethods(t *testing.T) {
	t.Parallel()

	for _, typ := range []reflect.Type{
		reflect.TypeOf(RawRow{}),
		reflect.TypeOf(&RawRow{}),
	} {
		if got := typ.NumMethod(); got != 0 {
			var names []string
			for i := 0; i < typ.NumMethod(); i++ {
				names = append(names, typ.Method(i).Name)
			}
			t.Errorf("%v 应零方法（纯数据载体），实际有 %d 个: %v", typ, got, names)
		}
	}
}

// TestRawRowDistinguishesExpenseAndCardCurrency 钉住「支出币种」与「卡片币种」
// 是正交两列：同一张 USD 卡上可以刷出一笔 EUR 消费。
//
// 合并成一个字段是个看似合理的简化（多数情况下两者相同），这条用例让那个简化
// 在第一次跑测试时就失败。
func TestRawRowDistinguishesExpenseAndCardCurrency(t *testing.T) {
	t.Parallel()

	eur, err := currency.Parse("EUR")
	if err != nil {
		t.Fatalf("构造 EUR 失败: %v", err)
	}
	usd, err := currency.Parse("USD")
	if err != nil {
		t.Fatalf("构造 USD 失败: %v", err)
	}

	row := RawRow{Currency: eur, CardCurrency: usd}
	if row.Currency.Equal(row.CardCurrency) {
		t.Error("支出币种与卡片币种应可不同（同一张 USD 卡可刷 EUR）")
	}
	if !row.Currency.Equal(eur) || !row.CardCurrency.Equal(usd) {
		t.Error("两个币种字段不应互相覆盖")
	}
}

// TestRawRowAmountAllowsNegative 钉住金额允许为负：退款与冲正走负数
// （终版 §四 3）。账单里的退款行直接映射过来，不该在端口层被拦或被取绝对值。
func TestRawRowAmountAllowsNegative(t *testing.T) {
	t.Parallel()

	refund, err := decimal.Parse("-88.50")
	if err != nil {
		t.Fatalf("解析负金额失败: %v", err)
	}
	row := RawRow{Amount: refund}
	if row.Amount.Sign() >= 0 {
		t.Error("RawRow.Amount 应保留负号（退款/冲正，终版 §四 3）")
	}
	if got := row.Amount.String(); got != "-88.5" && got != "-88.50" {
		t.Errorf("负金额应原样保留，实际 %q", got)
	}
}

// TestParseReturnsRowsAndError 走查 Parse 的两种返回形态，包括「空序列 + nil
// 错误」这一合法成功态（D-4 的「解析出 0 条」不是错误）。
func TestParseReturnsRowsAndError(t *testing.T) {
	t.Parallel()

	t.Run("成功返回行", func(t *testing.T) {
		t.Parallel()
		date, err := calendar.ParseDate("2026-01-15")
		if err != nil {
			t.Fatalf("构造日期失败: %v", err)
		}
		want := []RawRow{{Date: date, Counterparty: "超市"}}
		p := &stubParser{pt: enum.ParserTypeGenericCSV, rows: want}

		got, err := p.Parse(context.Background(), Input{})
		if err != nil {
			t.Fatalf("Parse 应成功，实际 %v", err)
		}
		if len(got) != 1 || got[0].Counterparty != "超市" {
			t.Errorf("返回的行不符: %+v", got)
		}
	})

	t.Run("零条也是成功", func(t *testing.T) {
		t.Parallel()
		p := &stubParser{pt: enum.ParserTypeGenericCSV, rows: nil}

		got, err := p.Parse(context.Background(), Input{})
		if err != nil {
			t.Errorf("解析出 0 条应为成功（D-4），实际报错 %v", err)
		}
		if len(got) != 0 {
			t.Errorf("应返回空序列，实际 %d 条", len(got))
		}
	})

	t.Run("失败返回分类错误", func(t *testing.T) {
		t.Parallel()
		p := &stubParser{
			pt:  enum.ParserTypeGenericCSV,
			err: NewFormatMismatchError("缺少表头"),
		}

		got, err := p.Parse(context.Background(), Input{})
		if err == nil {
			t.Fatal("应返回错误")
		}
		if got != nil {
			t.Errorf("失败时不应返回半批数据（取舍 5），实际 %d 条", len(got))
		}
		if key := errs.MsgKeyOf(err); key != KeyFormatMismatch {
			t.Errorf("错误应带文案键 %q，实际 %q", KeyFormatMismatch, key)
		}
	})
}

// TestParseRespectsContextCancellation 走查 ctx 的用途（取舍 6）：端口层不读
// ctx 里的值，但实现应能感知取消。本用例以一个检查 ctx.Err() 的实现回放这条
// 约定，确认接口签名足以支撑它——若 Parse 不收 ctx，这个实现根本写不出来。
func TestParseRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	cancelAware := parserFunc(func(ctx context.Context, _ Input) ([]RawRow, error) {
		if err := ctx.Err(); err != nil {
			return nil, WrapFormatMismatchError(err, "解析被取消")
		}
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := cancelAware.Parse(ctx, Input{})
	if err == nil {
		t.Fatal("ctx 已取消时实现应能感知并返回错误")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("错误链上应保留 context.Canceled，实际 %v", err)
	}
	if key := errs.MsgKeyOf(err); key != KeyFormatMismatch {
		t.Errorf("包装后仍应带文案键 %q，实际 %q", KeyFormatMismatch, key)
	}
}

// parserFunc 让一个函数满足 Parser，供只关心 Parse 行为的用例使用。
type parserFunc func(context.Context, Input) ([]RawRow, error)

func (f parserFunc) Type() enum.ParserType { return enum.ParserTypeGenericCSV }

func (f parserFunc) Parse(ctx context.Context, in Input) ([]RawRow, error) {
	return f(ctx, in)
}
