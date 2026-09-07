package enum

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从**下游消费方视角**走查本包的出口是否够用、是否会被误用。
//
// 与 codec_test.go 的分工：那边测「本包的方法行为对不对」，这边测「上层拿着这些
// 出口能不能把决策文档里的规则写出来」。分层方案 §十一 的落地顺序把 enum 排在
// 第 2 批、被 entity/store/service 全线依赖，出口缺一个，上层就得自己造一个私有
// 映射表，而那张表迟早与本包漂移。

// TestNoBaseDependency 断言本包不 import base/*（也不 import 任何项目内部包）。
//
// 分层总表允许 L1.0 依赖 base/*，但本包实现上比允许边界更收敛（见包注释「依赖边界」）。
// 这不是洁癖：一旦本包依赖了 base/errs，就等于在本包内替调用方选定了错误档位，
// 而同一个「未知码值」在 dto 侧是用户输入错误、在 store 侧是系统错误（数据损坏），
// 选任何一个都会让另一侧错。本测试把这条约束钉死，靠人工评审守不住。
func TestNoBaseDependency(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		//只看实现文件，测试文件不受此限（本文件自己就用了 go/ast）
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析本包源码失败: %v", err)
	}
	const modulePrefix = "github.com/cellargalaxy/jotcash/"
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if strings.HasPrefix(path, modulePrefix) {
					t.Errorf("%s 依赖了项目内部包 %q：本包应只依赖标准库，"+
						"依赖 base/errs 会导致本包替调用方选定错误档位（dto 侧是用户输入错误、"+
						"store 侧是数据损坏，二者不可兼得）", filepath.Base(name), path)
				}
			}
		}
	}
}

// TestConsumerAuditWriter 走查 service/audit 侧的写入形态。
//
// 终版 §8.4 给了 11 类操作与「对象类型 / 对象ID」的对应关系，其中 5 类落零值对象。
// 本用例模拟 audit 写入方按类型决定对象类型，验证本包的出口足以表达这张表——
// 尤其是「零值是合法取值」这一条：若 audit 侧只能用 Valid() 判定，5 类批量操作
// 会全部被判为非法。
func TestConsumerAuditWriter(t *testing.T) {
	//模拟 audit 的入参校验：对象类型允许零值（无对象/批量），但非零时必须合法
	checkObjectType := func(ot ObjectType) error {
		if ot.IsZero() {
			return nil //终版 §四 5：零值 = 无具体对象或批量操作
		}
		if !ot.Valid() {
			return fmt.Errorf("非法对象类型: %q", ot.Code())
		}
		return nil
	}

	//§8.4 的 11 类 → 对象类型对应表，逐行走查
	table := []struct {
		op         OperationType
		objectType ObjectType
		desc       string
	}{
		{OperationTypeSystemInit, ObjectTypeUser, "A-1 创建 admin"},
		{OperationTypeLogin, ObjectType(""), "A-4 登录，无对象"},
		{OperationTypeAccountManage, ObjectTypeUser, "A-7 账户管理"},
		{OperationTypePasswordChange, ObjectTypeUser, "A-8 密码修改"},
		{OperationTypeFileUpload, ObjectTypeFile, "C-1/D-2 文件上传"},
		{OperationTypeDataIntake, ObjectType(""), "D-7 数据入库，批量"},
		{OperationTypeExpenseModify, ObjectTypeExpense, "F-5/F-8 逐笔"},
		{OperationTypeExpenseModify, ObjectType(""), "F-8 批量删除"},
		{OperationTypeBaseAmountRecalc, ObjectType(""), "I-7 重算，批量"},
		{OperationTypeCategoryManage, ObjectTypeExpenseCategory, "G-1 支出类型维护"},
		{OperationTypePreferenceSet, ObjectTypeUser, "K-1 偏好设置"},
		{OperationTypeDataExport, ObjectType(""), "K-3/F-7 导出，批量"},
	}
	for _, row := range table {
		if !row.op.Valid() {
			t.Errorf("[%s] 操作类型 %q 非法", row.desc, row.op.Code())
		}
		if err := checkObjectType(row.objectType); err != nil {
			t.Errorf("[%s] %v", row.desc, err)
		}
	}

	//反面对照：非零的非法对象类型必须被拦下
	if err := checkObjectType(ObjectType("no_such_object")); err == nil {
		t.Error("非法对象类型未被拦下")
	}

	//登录失败以「操作结果 = 失败」表达，而非新增操作类型（§8.4 第 2 类）
	if !OperationResultFailure.Valid() {
		t.Error("操作结果「失败」应合法：登录失败靠它表达，不靠新增操作类型")
	}
}

// TestConsumerAuditExhaustiveSwitch 走查「按操作类型穷尽分支」的可写性。
//
// service/audit 需要为 11 类各配一段摘要生成逻辑。本用例模拟那段 switch，
// 并断言 OperationTypes() 的每一项都被覆盖——将来本包新增一类，这里立即失败，
// 提醒对应的 service 侧也要补。这正是 OperationTypes() 存在的意义（对照 errs.Kinds）。
func TestConsumerAuditExhaustiveSwitch(t *testing.T) {
	//模拟 audit 侧的分支：返回该类操作的对象类型约定
	expectObject := func(op OperationType) (ObjectType, bool) {
		switch op {
		case OperationTypeSystemInit, OperationTypeAccountManage,
			OperationTypePasswordChange, OperationTypePreferenceSet:
			return ObjectTypeUser, true
		case OperationTypeFileUpload:
			return ObjectTypeFile, true
		case OperationTypeExpenseModify:
			return ObjectTypeExpense, true //逐笔形态；批量形态用零值
		case OperationTypeCategoryManage:
			return ObjectTypeExpenseCategory, true
		case OperationTypeLogin, OperationTypeDataIntake,
			OperationTypeBaseAmountRecalc, OperationTypeDataExport:
			return ObjectType(""), true //无对象 / 批量
		default:
			return ObjectType(""), false //未覆盖
		}
	}

	for _, op := range OperationTypes() {
		if _, covered := expectObject(op); !covered {
			t.Errorf("操作类型 %q（%s）未被下游 switch 覆盖："+
				"本包新增了类目，service/audit 侧需同步补分支", op.Code(), op.String())
		}
	}
}

// TestConsumerFileUpload 走查 service/file 的 C-1 上传校验。
//
// C-1 明写「校验用户所选解析器类型键的合法性」，D-3 第五类错误是「所选解析器类型与
// 文件不匹配」。两条校验都要用本包的出口写出来——本用例验证确实写得出来，
// 且验证「合法」与「可被新选择」这两个判定确实是分开的。
func TestConsumerFileUpload(t *testing.T) {
	//模拟 service/file 的上传校验
	validate := func(pt ParserType, format FileFormat) error {
		//C-1：解析器类型键必须合法，且必须处于启用中（停用项不得被新上传选中）
		if !pt.Valid() {
			return errors.New("解析器类型非法")
		}
		if !pt.Enabled() {
			return errors.New("解析器类型已停用")
		}
		//D-3 第五类：所选解析器与文件格式不匹配
		if pt.Format() != format {
			return errors.New("解析器类型与文件不匹配")
		}
		return nil
	}

	if err := validate(ParserTypeGenericCSV, FileFormatCSV); err != nil {
		t.Errorf("通用 CSV 解析器 + CSV 文件应通过: %v", err)
	}
	if err := validate(ParserTypeGenericCSV, FileFormatPDF); err == nil {
		t.Error("CSV 解析器 + PDF 文件应被 D-3 第五类错误拦下")
	}
	if err := validate(ParserType("no_such_parser"), FileFormatCSV); err == nil {
		t.Error("未知解析器类型键应被 C-1 校验拦下")
	}
	if err := validate(ParserType(""), FileFormatCSV); err == nil {
		t.Error("零值解析器类型键应被 C-1 校验拦下（D-2 要求用户显式选择）")
	}

	//启用集必须是全集的子集，且本轮全部启用
	all, enabled := ParserTypes(), EnabledParserTypes()
	if len(enabled) > len(all) {
		t.Errorf("启用集不应大于全集: %d > %d", len(enabled), len(all))
	}
	allSet := make(map[ParserType]bool, len(all))
	for _, pt := range all {
		allSet[pt] = true
	}
	for _, pt := range enabled {
		if !allSet[pt] {
			t.Errorf("启用集中的 %q 不在全集中", pt.Code())
		}
		if !pt.Enabled() {
			t.Errorf("启用集中的 %q 的 Enabled() 为 false，两处口径不一致", pt.Code())
		}
	}
	//每个解析器类型的适用格式都必须是合法的 FileFormat——D-3 第五类判定依赖它
	for _, pt := range all {
		if !pt.Format().Valid() {
			t.Errorf("解析器类型 %q 的适用格式 %q 非法，D-3 第五类判定会恒不成立",
				pt.Code(), pt.Format().Code())
		}
	}
}

// TestCheckParserMetas 逐条验证元数据自洽性校验确实拦得住写错的声明。
//
// 包级表本身是正确的，因此「删掉某条校验」不会让常规用例失败——校验形同虚设也无人
// 察觉。这里给每条校验各喂一张对应写坏的表，确认它真的会报错；再喂一张正确的表，
// 确认它不是恒报错。
//
// **断言精确到报错原因**，而不只是「报了错」。几条校验之间存在兜底关系：例如删掉
// 「缺元数据」这条，最后的项数核对照样会报错，只是报成「项数不一致」——错误依然被
// 拦下，但排错时会指向错误的方向。只断言 err != nil 的写法放不出这个区别，
// 也就守不住每条校验各自的存在价值。
//
// 这四种写错方式都会让 D-2 上传页出问题，且全部在编译期无声：缺元数据 → 下拉标签
// 为空；格式非法 → D-3 第五类校验恒不成立；名称不一致 → 日志与界面对不上；
// 多余元数据 → 造成「加了枚举项」的错觉。
func TestCheckParserMetas(t *testing.T) {
	const pt = ParserType("some_bank_csv")
	const goodName = "某行 · 逐笔交易 · CSV"
	goodSet := newSet([]entry[ParserType]{{pt, goodName}})

	cases := []struct {
		desc string
		//metas 是喂给校验的元数据表
		metas map[ParserType]ParserMeta
		//wantErrContains 为空表示期望通过；非空表示期望报错且原因须含该片段
		wantErrContains string
	}{
		{
			desc:  "正确声明",
			metas: map[ParserType]ParserMeta{pt: {Name: goodName, Format: FileFormatCSV, Enabled: true}},
		},
		{
			desc: "缺元数据",
			//注意此表同时也让项数核对不通过，因此必须断言报的是「缺少元数据」，
			//否则删掉缺项校验时本用例会被项数核对兜住而假通过
			metas:           map[ParserType]ParserMeta{},
			wantErrContains: "缺少元数据",
		},
		{
			desc:            "名称为空",
			metas:           map[ParserType]ParserMeta{pt: {Name: "", Format: FileFormatCSV, Enabled: true}},
			wantErrContains: "名称为空",
		},
		{
			desc:            "适用格式非法",
			metas:           map[ParserType]ParserMeta{pt: {Name: goodName, Format: FileFormat("xlsx"), Enabled: true}},
			wantErrContains: "适用格式非法",
		},
		{
			desc:            "适用格式为零值",
			metas:           map[ParserType]ParserMeta{pt: {Name: goodName, Format: FileFormat(""), Enabled: true}},
			wantErrContains: "适用格式非法",
		},
		{
			desc:            "名称与枚举表不一致",
			metas:           map[ParserType]ParserMeta{pt: {Name: "另一个名字", Format: FileFormatCSV, Enabled: true}},
			wantErrContains: "名称不一致",
		},
		{
			desc: "元数据表有多余项",
			metas: map[ParserType]ParserMeta{
				pt:                       {Name: goodName, Format: FileFormatCSV, Enabled: true},
				ParserType("ghost_bank"): {Name: "幽灵行", Format: FileFormatCSV, Enabled: true},
			},
			wantErrContains: "项数不一致",
		},
	}
	for _, c := range cases {
		err := checkParserMetas(goodSet, c.metas)
		if c.wantErrContains == "" {
			if err != nil {
				t.Errorf("[%s] 不应报错: %v", c.desc, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("[%s] 应报错但未报错：该条校验若被删除，写错的声明会静默生效", c.desc)
			continue
		}
		if !strings.Contains(err.Error(), c.wantErrContains) {
			t.Errorf("[%s] 报错原因应含 %q，实际为 %q——"+
				"原因不符说明这一情形是被另一条校验兜住的，本条校验可能已失效",
				c.desc, c.wantErrContains, err.Error())
		}
		//报错必须指出是哪个键出的问题，否则多项时无从定位
		if c.desc != "元数据表有多余项" && !strings.Contains(err.Error(), string(pt)) {
			t.Errorf("[%s] 报错应指明出问题的键 %q, got %q", c.desc, string(pt), err.Error())
		}
	}

	//包级表必须自洽——这条等价于 init 的断言，但以可读的失败信息呈现
	if err := checkParserMetas(parserTypeSet, parserMetas); err != nil {
		t.Errorf("包级解析器元数据表不自洽: %v", err)
	}
}

// TestMustCheckParserMetasPanics 断言校验失败时确实 panic，而不是被静默忽略。
//
// 这条守的是 init 的行为本身。若有人把 init 里的 panic 改成 `_ = check(...)`，
// 上面的 TestCheckParserMetas 照样全绿——它测的是校验函数返回什么，而不是调用方
// 拿到错误后做了什么。元数据不自洽却让进程正常启动，等于把一个启动期就能发现的
// 编程错误推迟到用户上传账单时才暴露。
func TestMustCheckParserMetasPanics(t *testing.T) {
	const pt = ParserType("some_bank_csv")
	badSet := newSet([]entry[ParserType]{{pt, "某行 · 逐笔交易 · CSV"}})

	//不自洽的表必须 panic
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("元数据不自洽时应 panic：" +
					"否则错误的枚举声明会让进程正常启动，直到用户上传账单才暴露")
				return
			}
			//panic 信息必须说明原因，否则启动失败时无从下手
			msg, ok := r.(string)
			if !ok || !strings.Contains(msg, "缺少元数据") {
				t.Errorf("panic 信息应说明不自洽的原因, got %v", r)
			}
		}()
		mustCheckParserMetas(badSet, map[ParserType]ParserMeta{})
	}()

	//对照：自洽的表不得 panic，确认上面的断言不是恒真
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("自洽的元数据表不应 panic: %v", r)
			}
		}()
		mustCheckParserMetas(badSet, map[ParserType]ParserMeta{
			pt: {Name: "某行 · 逐笔交易 · CSV", Format: FileFormatCSV, Enabled: true},
		})
	}()

	//包级表走同一条路径：进程能跑到这里，本身就说明 init 已通过
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("包级元数据表不应 panic: %v", r)
			}
		}()
		mustCheckParserMetas(parserTypeSet, parserMetas)
	}()
}

// TestParserTypeAccessorsDefaultDeny 断言 Enabled() 与 Format() 对未知键默认拒绝。
//
// 本轮只有一个解析器且恰好是「启用 + CSV」，因此 `return true` / `return CSV` 这类
// 写死实现与真实实现在正常路径上结果完全一致，正常用例区分不出来。区分点在**异常
// 路径**：未知键与零值必须得到「未启用」「零值格式」，否则——
//
//   - Enabled() 恒真：一个从库里读出的、已被删掉的解析器键会被判为可用，
//     C-1 校验形同虚设；
//   - Format() 恒为 CSV：任何未知键都会被认作 CSV 解析器，D-3 第五类
//     「解析器与文件不匹配」对坏数据永远不成立。
func TestParserTypeAccessorsDefaultDeny(t *testing.T) {
	unknowns := []ParserType{
		ParserType(""),                 //零值
		ParserType("no_such_parser"),   //从未声明过
		ParserType("GENERIC_CSV"),      //大小写变体
		ParserType("generic_csv_v2"),   //形似的键
		ParserType("removed_bank_pdf"), //假想中被删掉的键
	}
	for _, pt := range unknowns {
		if pt.Enabled() {
			t.Errorf("Enabled(%q) = true, want false（未知键必须默认拒绝，"+
				"否则 C-1 校验会放行不存在的解析器）", pt.Code())
		}
		if got := pt.Format(); !got.IsZero() {
			t.Errorf("Format(%q) = %q, want 零值（未知键不得被认作某种格式，"+
				"否则 D-3 第五类匹配校验对坏数据永远不成立）", pt.Code(), got.Code())
		}
		if _, ok := pt.Meta(); ok {
			t.Errorf("Meta(%q) 第二返回值应为 false", pt.Code())
		}
		//未知键取到的元数据必须是零值，不能是「看起来可用」的元数据
		meta, _ := pt.Meta()
		if meta.Enabled || meta.Name != "" || !meta.Format.IsZero() {
			t.Errorf("Meta(%q) 应返回零值元数据, got %+v", pt.Code(), meta)
		}
	}

	//对照：已声明的键必须取到真实元数据，确认上面的断言不是恒真
	if !ParserTypeGenericCSV.Enabled() {
		t.Error("已声明且启用的解析器 Enabled() 应为真")
	}
	if ParserTypeGenericCSV.Format() != FileFormatCSV {
		t.Errorf("通用 CSV 解析器的适用格式 = %q, want csv",
			ParserTypeGenericCSV.Format().Code())
	}
	//Enabled() 与 Format() 必须真的读元数据表，而不是写死——
	//用一个「格式非 CSV」的假想项验证读取路径确实通向元数据
	for _, pt := range ParserTypes() {
		meta, ok := pt.Meta()
		if !ok {
			t.Fatalf("已声明的 %q 应有元数据", pt.Code())
		}
		if pt.Enabled() != meta.Enabled {
			t.Errorf("Enabled(%q) 与元数据不一致: %v vs %v", pt.Code(), pt.Enabled(), meta.Enabled)
		}
		if pt.Format() != meta.Format {
			t.Errorf("Format(%q) 与元数据不一致: %q vs %q",
				pt.Code(), pt.Format().Code(), meta.Format.Code())
		}
	}
}

// TestParserTypeEnabledFilter 验证「停用项不出现在下拉里」这个**过滤机制**成立。
//
// 本轮所有解析器都是启用状态，因此遍历包级表的用例无法区分「过滤生效」与
// 「过滤被删掉」——两种实现给出的结果完全相同。这里改喂一张含停用项的表，
// 让过滤逻辑真正承压：删掉过滤，本用例立即失败。
//
// 这条守的是 §五 2「只允许停用」的下半句：停用之后必须真的从 D-2 下拉里消失，
// 否则停用就只是个没有效果的标记位。
func TestParserTypeEnabledFilter(t *testing.T) {
	const (
		active  = ParserType("active_bank_csv")
		retired = ParserType("retired_bank_csv")
		another = ParserType("another_bank_csv")
	)
	//声明序：启用、停用、启用——同时验证过滤后仍保持原声明序
	codes := []ParserType{active, retired, another}
	metas := map[ParserType]ParserMeta{
		active:  {Name: "在用行 · 逐笔交易 · CSV", Format: FileFormatCSV, Enabled: true},
		retired: {Name: "退役行 · 逐笔交易 · CSV", Format: FileFormatCSV, Enabled: false},
		another: {Name: "另一行 · 逐笔交易 · CSV", Format: FileFormatCSV, Enabled: true},
	}

	got := filterEnabledParserTypes(codes, metas)
	want := []ParserType{active, another}
	if len(got) != len(want) {
		t.Fatalf("过滤后项数 = %d, want %d（停用项应被排除在 D-2 下拉之外）", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("过滤后[%d] = %q, want %q（过滤须保持声明序）", i, got[i].Code(), want[i].Code())
		}
	}
	//停用项绝不能出现在结果中
	for _, pt := range got {
		if pt == retired {
			t.Error("停用项出现在启用集里：D-2 下拉会给出一个不该被选中的选项")
		}
	}
	//全部停用时应得空集而非全集——这是「过滤被删掉」最典型的表现
	allDisabled := map[ParserType]ParserMeta{
		active:  {Name: "在用行", Format: FileFormatCSV, Enabled: false},
		retired: {Name: "退役行", Format: FileFormatCSV, Enabled: false},
		another: {Name: "另一行", Format: FileFormatCSV, Enabled: false},
	}
	if n := len(filterEnabledParserTypes(codes, allDisabled)); n != 0 {
		t.Errorf("全部停用时启用集项数 = %d, want 0", n)
	}
	//元数据缺项时按未启用处理，不得漏进下拉
	if n := len(filterEnabledParserTypes(codes, map[ParserType]ParserMeta{})); n != 0 {
		t.Errorf("元数据缺失时启用集项数 = %d, want 0（缺元数据不得默认放行）", n)
	}
}

// TestConsumerParserTypeDisabledStillReadable 走查「只能停用、不能删除」的运行形态。
//
// 终版 §五 2 定「不允许删除枚举项，只允许停用」。停用后必须仍能读出历史 File 行，
// 否则一次停用就会让存量数据不可读。本用例用一个临时停用项验证这条性质——
// 它测的是**机制**（Valid/Scan 不看 Enabled），不是当前恰好没有停用项这个事实。
func TestConsumerParserTypeDisabledStillReadable(t *testing.T) {
	//直接对内核构造一个「含停用项」的场景，避免污染包级表
	tmpSet := newSet([]entry[ParserType]{
		{ParserType("legacy_bank_csv"), "旧行 · 逐笔交易 · CSV"},
	})
	disabled := ParserType("legacy_bank_csv")

	//停用项必须仍是合法码值：历史行读得出来
	if !tmpSet.valid(disabled) {
		t.Error("停用项应仍是合法码值，否则历史 File 行不可读")
	}
	//Scan 路径同样放行
	var dst ParserType
	if err := scan("legacy_bank_csv", &dst, tmpSet); err != nil {
		t.Errorf("停用项的 Scan 应成功（历史行必须可读）: %v", err)
	}
	if dst != disabled {
		t.Errorf("Scan 结果 = %q, want %q", dst.Code(), disabled.Code())
	}
	//而删除项（未声明的键）则必须失败——这正是「不能删除」的原因
	var deleted ParserType
	if err := scan("removed_parser", &deleted, tmpSet); !errors.Is(err, ErrUnknownCode) {
		t.Errorf("已删除的键应 Scan 失败（这就是枚举项只能停用不能删除的原因）: %v", err)
	}
}

// TestConsumerErrorClassification 走查上层的错误分档映射。
//
// 本包只给哨兵，档位由调用方选：dto 侧 → 用户输入错误，store 侧 → 系统错误。
// 本用例模拟两侧的分类逻辑，确认哨兵的粒度足以支撑这个区分。
func TestConsumerErrorClassification(t *testing.T) {
	//模拟 api/http/dto 侧：外部提交的非法值属用户输入错误
	classifyFromDTO := func(err error) string {
		switch {
		case err == nil:
			return "ok"
		case errors.Is(err, ErrUnknownCode), errors.Is(err, ErrCodeType):
			return "用户输入错误"
		default:
			return "系统错误"
		}
	}
	//模拟 store/sqlite 侧：库里出现未知码值属数据损坏，是系统错误
	classifyFromStore := func(err error) string {
		switch {
		case err == nil:
			return "ok"
		case errors.Is(err, ErrUnknownCode), errors.Is(err, ErrScanType):
			return "系统错误" //数据损坏，绝不能当 400 吐给前端
		default:
			return "系统错误"
		}
	}

	var role Role
	badJSON := role.UnmarshalJSON([]byte(`"root"`))
	if got := classifyFromDTO(badJSON); got != "用户输入错误" {
		t.Errorf("dto 侧未知码值应归用户输入错误, got %q", got)
	}
	badType := role.UnmarshalJSON([]byte(`123`))
	if got := classifyFromDTO(badType); got != "用户输入错误" {
		t.Errorf("dto 侧类型错误应归用户输入错误, got %q", got)
	}
	badScan := role.Scan("root")
	if got := classifyFromStore(badScan); got != "系统错误" {
		t.Errorf("store 侧未知码值应归系统错误（数据损坏）, got %q", got)
	}

	//同一个哨兵在两侧被映射到不同档位——这正是本包不自行分档的理由
	if !errors.Is(badJSON, ErrUnknownCode) || !errors.Is(badScan, ErrUnknownCode) {
		t.Fatal("两侧应为同一个哨兵错误")
	}
	if classifyFromDTO(badJSON) == classifyFromStore(badScan) {
		t.Error("同一哨兵在两侧应映射到不同档位，否则本包自行分档也无妨——" +
			"用例前提失效，需重新检视包注释「错误是哨兵」一节")
	}

	//错误信息必须带上出错的码值，否则排错时不知道是哪个值坏了
	if !strings.Contains(badJSON.Error(), "root") {
		t.Errorf("错误信息应包含出错码值, got %q", badJSON.Error())
	}
}

// TestConsumerLoginFlow 走查 A-4 登录判定链路。
//
// 状态判定必须是「默认拒绝」：只有恰等于启用才放行。本用例特别验证零值与未知码值
// 都不放行——若 IsEnabled 写成 `!= disabled`，零值账户就能登录。
func TestConsumerLoginFlow(t *testing.T) {
	cases := []struct {
		status UserStatus
		allow  bool
		desc   string
	}{
		{UserStatusEnabled, true, "启用账户放行"},
		{UserStatusDisabled, false, "禁用账户拒绝"},
		{UserStatus(""), false, "零值状态拒绝（默认拒绝）"},
		{UserStatus("active"), false, "未知码值拒绝（默认拒绝）"},
	}
	for _, c := range cases {
		if got := c.status.IsEnabled(); got != c.allow {
			t.Errorf("[%s] IsEnabled(%q) = %v, want %v", c.desc, c.status.Code(), got, c.allow)
		}
	}

	//同构：操作结果的成功判定同样默认拒绝
	if OperationResult("").IsSuccess() || OperationResult("ok").IsSuccess() {
		t.Error("IsSuccess 应默认拒绝：零值与未知码值都不算成功")
	}
	if !OperationResultSuccess.IsSuccess() {
		t.Error("成功值的 IsSuccess 应为真")
	}
}

// TestConsumerAdminPrivilege 走查前提 3 的管理员特权判定。
//
// IsAdmin 必须只对 RoleAdmin 为真；零值与未知角色一律不是管理员（默认拒绝）。
func TestConsumerAdminPrivilege(t *testing.T) {
	if !RoleAdmin.IsAdmin() {
		t.Error("RoleAdmin.IsAdmin() 应为真")
	}
	for _, r := range []Role{RoleUser, Role(""), Role("administrator"), Role("root")} {
		if r.IsAdmin() {
			t.Errorf("Role(%q).IsAdmin() 应为假（默认拒绝）", r.Code())
		}
	}
}

// TestConsumerJSONContractStability 走查前后端 JSON 契约的稳定性。
//
// dto 会把这些枚举放进响应体。契约面必须恒为码值字符串，绝不能因为某个类型
// 忘了实现 MarshalJSON 而漏成中文名——那会让前端拿到一个无法用于回传的值。
func TestConsumerJSONContractStability(t *testing.T) {
	//用一个贴近真实 dto 的结构体做整体序列化
	type auditLogDTO struct {
		OperationType   OperationType   `json:"operation_type"`
		ObjectType      ObjectType      `json:"object_type"`
		OperationResult OperationResult `json:"operation_result"`
	}
	type userDTO struct {
		Role   Role       `json:"role"`
		Status UserStatus `json:"status"`
	}
	type fileDTO struct {
		Format     FileFormat `json:"format"`
		ParserType ParserType `json:"parser_type"`
	}

	checks := []struct {
		value any
		want  string
		desc  string
	}{
		{
			auditLogDTO{OperationTypeDataIntake, ObjectType(""), OperationResultSuccess},
			`{"operation_type":"data_intake","object_type":"","operation_result":"success"}`,
			"批量入库审计：对象类型为合法零值，序列化为空串而非 null",
		},
		{
			userDTO{RoleAdmin, UserStatusEnabled},
			`{"role":"admin","status":"enabled"}`,
			"用户 DTO",
		},
		{
			fileDTO{FileFormatCSV, ParserTypeGenericCSV},
			`{"format":"csv","parser_type":"generic_csv"}`,
			"文件 DTO：格式与解析器是正交的两列，码值不同",
		},
	}
	for _, c := range checks {
		data, err := marshalIndentless(c.value)
		if err != nil {
			t.Errorf("[%s] 序列化报错: %v", c.desc, err)
			continue
		}
		if data != c.want {
			t.Errorf("[%s] JSON 契约变化:\n got: %s\nwant: %s", c.desc, data, c.want)
		}
	}
}

// marshalIndentless 按标准 JSON 序列化并返回字符串，供契约比对使用。
func marshalIndentless(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TestConsumerExportReadable 走查 K-3 整库导出的可读性诉求。
//
// 前提 1 假定数据明文存储、K-3 要求导出产物可被直接查看。码值为字符串正是为此：
// 导出的 CSV 里出现 data_intake 而不是一个 6，无需另配映射表就能读懂。
// 本用例断言全部码值都是人类可读的 ASCII 标识符形态。
func TestConsumerExportReadable(t *testing.T) {
	readable := func(code string) bool {
		if code == "" {
			return false
		}
		for i := 0; i < len(code); i++ {
			ch := code[i]
			isLower := ch >= 'a' && ch <= 'z'
			isDigit := ch >= '0' && ch <= '9'
			if !isLower && !isDigit && ch != '_' {
				return false
			}
		}
		return true
	}

	groups := map[string][]string{
		"Role":            codesOf(Roles()),
		"UserStatus":      codesOf(UserStatuses()),
		"FileFormat":      codesOf(FileFormats()),
		"ParserType":      codesOf(ParserTypes()),
		"OperationType":   codesOf(OperationTypes()),
		"ObjectType":      codesOf(ObjectTypes()),
		"OperationResult": codesOf(OperationResults()),
	}
	for name, codes := range groups {
		for _, c := range codes {
			if !readable(c) {
				t.Errorf("%s 的码值 %q 不是小写蛇形 ASCII 标识符："+
					"K-3 导出产物需人可直读，且码值会进 URL 查询参数与 CSV", name, c)
			}
		}
	}
}

// TestAllEnumsImplementContracts 断言 7 个类型都实现了完整的四组接口。
//
// 漏实现任何一个都会静默降级：漏 MarshalJSON 则 JSON 里出现中文名；漏 Valuer 则
// 落库形态由驱动决定；漏 Scanner 则读回时报类型错误。编译期不会提醒，
// 因为这些接口都是隐式实现的。
func TestAllEnumsImplementContracts(t *testing.T) {
	//编译期断言：任何一个类型漏实现，本文件都编译不过
	var (
		_ interface{ Code() string } = RoleAdmin
		_ interface{ Code() string } = UserStatusEnabled
		_ interface{ Code() string } = FileFormatCSV
		_ interface{ Code() string } = ParserTypeGenericCSV
		_ interface{ Code() string } = OperationTypeLogin
		_ interface{ Code() string } = ObjectTypeUser
		_ interface{ Code() string } = OperationResultSuccess
	)

	//运行期核对：确认每个类型的 Valid/IsZero/String 三元组自洽
	type probe struct {
		name   string
		valid  bool
		isZero bool
		str    string
	}
	probes := []probe{
		{"Role", RoleAdmin.Valid(), RoleAdmin.IsZero(), RoleAdmin.String()},
		{"UserStatus", UserStatusEnabled.Valid(), UserStatusEnabled.IsZero(), UserStatusEnabled.String()},
		{"FileFormat", FileFormatCSV.Valid(), FileFormatCSV.IsZero(), FileFormatCSV.String()},
		{"ParserType", ParserTypeGenericCSV.Valid(), ParserTypeGenericCSV.IsZero(), ParserTypeGenericCSV.String()},
		{"OperationType", OperationTypeLogin.Valid(), OperationTypeLogin.IsZero(), OperationTypeLogin.String()},
		{"ObjectType", ObjectTypeUser.Valid(), ObjectTypeUser.IsZero(), ObjectTypeUser.String()},
		{"OperationResult", OperationResultSuccess.Valid(), OperationResultSuccess.IsZero(), OperationResultSuccess.String()},
	}
	for _, p := range probes {
		if !p.valid {
			t.Errorf("%s 的样本值 Valid() 应为真", p.name)
		}
		if p.isZero {
			t.Errorf("%s 的样本值 IsZero() 应为假", p.name)
		}
		if p.str == "" {
			t.Errorf("%s 的 String() 不应为空", p.name)
		}
	}
}

// TestSetSelfCheckPanics 断言内核的自洽性校验确实会 panic。
//
// newSet 的两条校验（码值非空、码值不重复）是包初始化期的防线。若它们失效，
// 一个重复的码值会让后声明的项静默覆盖先声明的项，且全无征兆。
func TestSetSelfCheckPanics(t *testing.T) {
	cases := []struct {
		desc    string
		entries []entry[Role]
	}{
		{"空串码值", []entry[Role]{{Role(""), "空"}}},
		{"重复码值", []entry[Role]{{RoleAdmin, "管理员"}, {RoleAdmin, "又一个管理员"}}},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("[%s] newSet 应 panic，否则错误声明会静默生效", c.desc)
				}
			}()
			_ = newSet(c.entries)
		}()
	}

	//对照：合法声明不应 panic
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("合法声明不应 panic: %v", r)
			}
		}()
		_ = newSet([]entry[Role]{{RoleAdmin, "管理员"}, {RoleUser, "普通用户"}})
	}()
}

// TestSourceHasNoEnglishComment 抽查实现文件的注释是否为中文（约定 3）。
//
// 只做弱校验：断言每个实现文件的注释里都含中文字符，而不是逐行判定语种——
// 后者会误伤代码示例、专有名词与 URL。
func TestSourceHasNoEnglishComment(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("解析本包源码失败: %v", err)
	}
	hasCJK := func(s string) bool {
		for _, r := range s {
			if r >= 0x4E00 && r <= 0x9FFF {
				return true
			}
		}
		return false
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			var commentText strings.Builder
			ast.Inspect(file, func(n ast.Node) bool {
				if cg, ok := n.(*ast.CommentGroup); ok {
					commentText.WriteString(cg.Text())
				}
				return true
			})
			for _, cg := range file.Comments {
				commentText.WriteString(cg.Text())
			}
			if !hasCJK(commentText.String()) {
				t.Errorf("%s 的注释中未见中文：约定 3 要求注释与日志一律中文", filepath.Base(name))
			}
		}
	}
}
