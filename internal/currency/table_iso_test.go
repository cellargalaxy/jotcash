package currency

// 策展表与 **ISO 4217 官方基准**的逐条比对。
//
// 基准取 `github.com/bojanz/currency`（v1.4.4）：它的币种数据由 gen.go 直接从 ISO 官方
// list-one.xml 生成，`GetDigits` 返回的就是 **minor unit**。本包**只在单测里**依赖它，
// 生产代码不依赖——理由见 currency.go 包注释「为什么枚举表必须在仓库内」：
// 第三方库会随版本删币种（终版 §五 1 要求「只能停用不能删除」），
// 小数位数若是运行时查库得来，一次库升级就可能静默改掉 8.2 摊分的均分基准。
//
// 本文件要防的是另一半风险：**表里的 Scale 一开始就填错**。填错不会有任何报错，
// 只会让该币种的每一次摊分与折算都算错一点点（准则 2 点名的「算错就静默出错」），
// 因此必须有一个独立于本仓库的权威基准来逐条对账。
//
// 两个方向都查：
//
//	正向：策展表每一项的 Scale 必须等于 ISO 的 minor unit，且必须是 ISO 认可的流通货币；
//	反向：刻意排除的那几类代码（非流通 / 非 ISO / 4 位记账单位）必须确实不在表内，
//	      防止后人「顺手补一个」把 table.go 的策展纪律破掉。

import (
	"testing"

	iso "github.com/bojanz/currency"
)

// TestTableMatchesISO4217 策展表逐条对齐 ISO 4217 的 minor unit 与流通性。
//
// 这条用例失败有且只有两种可能，且修法完全不同：
//   - 表里的 Scale 填错了 → 改 table.go（但若该币种已在生产库里存过明细，
//     按 table.go 纪律 2，正确做法是新增新代码 + 停用旧代码，而不是改 Scale）；
//   - ISO 真的改了该币种的 minor unit → 同样按纪律 2 走「新增 + 停用」，不是就地改。
func TestTableMatchesISO4217(t *testing.T) {
	for _, cur := range table {
		code := cur.Code.String()

		// ① 必须是 ISO 认可的流通货币。
		// IsValid=false 的典型是已退出流通（ZWL / SLL / MRO / VEF / STD / BYR / HRK）
		// 与非货币代码（XAU 黄金 / XDR 特别提款权 / XXX 无币种），二者都不该进策展表。
		if !iso.IsValid(code) {
			t.Errorf("%s 不是 ISO 4217 认可的流通货币，不应进入策展表", code)
			continue
		}

		// ② Scale 必须等于 ISO 的 minor unit（**不是** CLDR 展示位数，见下方 TestISONotCLDR）。
		digits, ok := iso.GetDigits(code)
		if !ok {
			t.Errorf("%s 无法从 ISO 基准取到 minor unit", code)
			continue
		}
		if int32(digits) != cur.Scale {
			t.Errorf("%s 的小数位数与 ISO 4217 不符：表内 %d，ISO minor unit %d"+
				"（它是 8.2 均分与不变式 1a 舍入的基准，填错会静默算错）",
				code, cur.Scale, digits)
		}
	}
}

// TestExcludedCodesStayExcluded table.go「币种范围」一节刻意排除的四类代码必须确实不在表内。
//
// 写成用例而非只写注释：注释挡不住后人「顺手加一行」。这四类各自都会破坏一条既有口径，
// 见每组的说明。
func TestExcludedCodesStayExcluded(t *testing.T) {
	groups := []struct {
		reason string
		codes  []Code
	}{
		{
			// 收录会与 ISO「流通货币」口径打架，且这些代码的 minor unit 无从取得。
			reason: "非流通 / 非货币代码（贵金属、特别提款权、无币种）",
			codes:  []Code{"XAU", "XAG", "XDR", "XXX"},
		},
		{
			// 4 位记账单位（unit of account），不是可支付货币，不会出现在个人银行账单上；
			// 且会把 base/decimal 的本位币存储档（2 位）与 store 列宽决策一起抬高。
			reason: "4 位小数的记账单位（CLF 智利 UF / UYW 乌拉圭名义工资指数）",
			codes:  []Code{"CLF", "UYW"},
		},
		{
			// 非 ISO 4217 代码：ISO 侧人民币只有 CNY。账单上出现 CNH 应由 parser 归一到 CNY。
			reason: "非 ISO 4217 代码",
			codes:  []Code{"CNH"},
		},
		{
			// 已退出流通 / 已被取代：它们正是「第三方库会删币种」风险的活体样本
			// （currency.go 包注释列举的那一批），也是 TestValidVsEnabled 借用的停用样本来源。
			reason: "已退出流通或已被新代码取代",
			codes:  []Code{"ZWL", "SLL", "MRO", "VEF", "STD", "BYR", "HRK"},
		},
	}
	for _, g := range groups {
		for _, code := range g.codes {
			if Valid(code) {
				t.Errorf("%s 属「%s」，不应被收录进策展表", code, g.reason)
			}
		}
	}
}

// TestISONotCLDR 钉死「基准是 ISO minor unit，不是 CLDR 展示位数」这条选择。
//
// 存在的意义是防一类很自然的「优化」：本仓库的模块图里已经有 `golang.org/x/text`
// （被若干间接依赖引入），于是有人会想「何必多一个 bojanz，用已有的
// x/text/currency 取小数位数不就行了」。答案是**不行**——`x/text` 的 Kind.Rounding
// 返回 CLDR 展示位数，与 ISO minor unit 不是一件事：实测 149 个可比对的流通币种里
// **22 个不一致**（IDR 2→0、IQD 3→0、PKR 2→0、AMD 2→0、AFN、ALL、COP、GYD、IRR、
// KPW、LAK、LBP、MGA、MMK、MNT、MUR、RSD、SOS、SYP、TZS、UZS、YER），
// 且其 CLDR 数据停在较早版本。
//
// 本用例不 import x/text（不为一条断言给生产模块图添依赖），而是**直接钉住那些
// 已知分歧币种在本表内的取值必须是 ISO 值**。若将来有人把 Scale 改成 CLDR 口径，
// IDR 会从 2 变 0，这里当场失败。
func TestISONotCLDR(t *testing.T) {
	// 本表收录的币种中，ISO 与 CLDR 展示位数已知分歧的那一个。
	// IDR：ISO minor unit = 2，CLDR 展示位数 = 0。
	const code = IDR
	cur, ok := Get(code)
	if !ok {
		t.Fatalf("前置条件变了：%s 已不在策展表内，请更新本用例", code)
	}
	if cur.Scale != 2 {
		t.Errorf("%s 的小数位数应为 ISO minor unit 2，实际 %d"+
			"（若为 0，说明基准被误换成了 CLDR 展示位数）", code, cur.Scale)
	}
	// 与 ISO 基准再对一次，确保上面的 2 不是写死的巧合
	digits, _ := iso.GetDigits(code.String())
	if int32(digits) != cur.Scale {
		t.Errorf("%s 与 ISO 基准不符：表内 %d，ISO %d", code, cur.Scale, digits)
	}
}

// TestScaleWithinISORange 表内小数位数必须落在 ISO 4217 的实际取值域 [0,4] 内。
// init 里已有同样的 panic 校验，这里让它表现为一条具名用例（理由同 TestTableIntegrity）。
func TestScaleWithinISORange(t *testing.T) {
	for _, cur := range table {
		if cur.Scale < 0 || cur.Scale > 4 {
			t.Errorf("%s 的小数位数 %d 超出 ISO 4217 取值域 [0,4]", cur.Code, cur.Scale)
		}
	}
}
