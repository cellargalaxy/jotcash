package currency

import (
	"errors"
	"testing"
)

// TestParse 覆盖解析的成功与三类失败。
func TestParse(t *testing.T) {
	t.Run("已知币种解析成功", func(t *testing.T) {
		for _, code := range []string{"CNY", "USD", "JPY", "KWD"} {
			c, err := Parse(code)
			if err != nil {
				t.Fatalf("Parse(%q) 返回错误: %v", code, err)
			}
			if c.String() != code {
				t.Errorf("Parse(%q).String() = %q, 期望 %q", code, c.String(), code)
			}
			if c.IsZero() {
				t.Errorf("Parse(%q) 得到零值", code)
			}
		}
	})

	t.Run("空串返回 ErrEmptyCode", func(t *testing.T) {
		c, err := Parse("")
		if !errors.Is(err, ErrEmptyCode) {
			t.Fatalf("Parse(\"\") 错误 = %v, 期望 ErrEmptyCode", err)
		}
		if !c.IsZero() {
			t.Error("解析失败时应返回零值")
		}
	})

	t.Run("未知代码返回 ErrUnknownCode", func(t *testing.T) {
		// 含大小写变体：本包不做大小写归一，理由见 Parse 注释。
		for _, bad := range []string{"XXX", "ABC", "cny", "Cny", "CN", "CNYY", "人民币", " CNY", "CNY "} {
			c, err := Parse(bad)
			if !errors.Is(err, ErrUnknownCode) {
				t.Errorf("Parse(%q) 错误 = %v, 期望 ErrUnknownCode", bad, err)
			}
			if !c.IsZero() {
				t.Errorf("Parse(%q) 失败时应返回零值", bad)
			}
		}
	})

	t.Run("错误信息带上出错的代码", func(t *testing.T) {
		// 排错时需要知道是哪个代码出的错，wrap 的上下文不能丢。
		_, err := Parse("XXX")
		if err == nil || !contains(err.Error(), "XXX") {
			t.Errorf("错误信息 %v 未包含出错的代码", err)
		}
	})
}

// TestMustParse 覆盖 panic 分支。
func TestMustParse(t *testing.T) {
	t.Run("合法代码不 panic", func(t *testing.T) {
		if got := MustParse("CNY").String(); got != "CNY" {
			t.Errorf("MustParse(\"CNY\").String() = %q", got)
		}
	})

	t.Run("非法代码 panic", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("MustParse(\"XXX\") 未 panic")
			}
		}()
		MustParse("XXX")
	})
}

// TestParseBase 覆盖 T38④「非候选一律拒绝」的三类失败分档。
//
// 三类必须分档而不是笼统报一个错：service/preference 要按档给不同文案。
func TestParseBase(t *testing.T) {
	t.Run("候选币种通过", func(t *testing.T) {
		for _, code := range []string{"CNY", "USD", "JPY"} {
			c, err := ParseBase(code)
			if err != nil {
				t.Errorf("ParseBase(%q) 返回错误: %v", code, err)
			}
			if !c.CanBeBase() {
				t.Errorf("ParseBase(%q) 返回的币种不满足候选口径", code)
			}
		}
	})

	t.Run("3 位小数币种返回 ErrNotBaseCandidate", func(t *testing.T) {
		c, err := ParseBase("KWD")
		if !errors.Is(err, ErrNotBaseCandidate) {
			t.Fatalf("ParseBase(\"KWD\") 错误 = %v, 期望 ErrNotBaseCandidate", err)
		}
		if !c.IsZero() {
			t.Error("校验失败时应返回零值")
		}
		// 错误信息要带上判据取值，否则排查「为什么这个币种不能选」时只能猜。
		if !contains(err.Error(), "scale=3") {
			t.Errorf("错误信息 %v 未包含小数位数", err)
		}
	})

	t.Run("空串与未知代码沿用原档", func(t *testing.T) {
		if _, err := ParseBase(""); !errors.Is(err, ErrEmptyCode) {
			t.Errorf("ParseBase(\"\") 错误 = %v, 期望 ErrEmptyCode", err)
		}
		if _, err := ParseBase("XXX"); !errors.Is(err, ErrUnknownCode) {
			t.Errorf("ParseBase(\"XXX\") 错误 = %v, 期望 ErrUnknownCode", err)
		}
	})
}

// TestIsKnown 覆盖判定函数。
func TestIsKnown(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"CNY", true},
		{"KWD", true}, // 3 位币种也是「已知」
		{"", false},   // 空串不是合法币种（bojanz/currency 在此返回 true，本包不照抄）
		{"XXX", false},
		{"cny", false},
	}
	for _, tc := range cases {
		if got := IsKnown(tc.code); got != tc.want {
			t.Errorf("IsKnown(%q) = %v, 期望 %v", tc.code, got, tc.want)
		}
	}
}

// TestZeroValue 覆盖零值在全部访问器上的行为。
//
// 零值必须处处可用不 panic：实体以零值创建（卡片币种「解析不出则留空」），
// 上层不应被迫先判空再取值。
func TestZeroValue(t *testing.T) {
	var c Code

	if !c.IsZero() {
		t.Error("零值 IsZero() 应为 true")
	}
	if got := c.String(); got != "" {
		t.Errorf("零值 String() = %q, 期望空串", got)
	}
	if got := c.Name(); got != "" {
		t.Errorf("零值 Name() = %q, 期望空串", got)
	}
	if got := c.NameKey(); got != "" {
		t.Errorf("零值 NameKey() = %q, 期望空串", got)
	}
	if got := c.Symbol(); got != "" {
		t.Errorf("零值 Symbol() = %q, 期望空串", got)
	}
	if got := c.Scale(); got != 0 {
		t.Errorf("零值 Scale() = %d, 期望 0", got)
	}
	if c.Enabled() {
		t.Error("零值 Enabled() 应为 false")
	}
	if got := c.RateSource(); got != "" {
		t.Errorf("零值 RateSource() = %q, 期望空串", got)
	}
	if c.CanBeBase() {
		t.Error("零值 CanBeBase() 应为 false")
	}
	if !c.Equal(Code{}) {
		t.Error("零值应与零值相等")
	}
}

// TestAttributes 覆盖六项属性的取值。
func TestAttributes(t *testing.T) {
	cases := []struct {
		code    string
		name    string
		symbol  string
		scale   int32
		nameKey string
	}{
		{"CNY", "人民币", "¥", 2, "currency.name.CNY"},
		{"USD", "美元", "US$", 2, "currency.name.USD"},
		{"JPY", "日元", "JP¥", 0, "currency.name.JPY"},
		{"KWD", "科威特第纳尔", "KWD", 3, "currency.name.KWD"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			c := MustParse(tc.code)
			if got := c.String(); got != tc.code {
				t.Errorf("String() = %q, 期望 %q", got, tc.code)
			}
			if got := c.Name(); got != tc.name {
				t.Errorf("Name() = %q, 期望 %q", got, tc.name)
			}
			if got := c.Symbol(); got != tc.symbol {
				t.Errorf("Symbol() = %q, 期望 %q", got, tc.symbol)
			}
			if got := c.Scale(); got != tc.scale {
				t.Errorf("Scale() = %d, 期望 %d", got, tc.scale)
			}
			if got := c.NameKey(); got != tc.nameKey {
				t.Errorf("NameKey() = %q, 期望 %q", got, tc.nameKey)
			}
			if !c.Enabled() {
				t.Error("Enabled() 应为 true")
			}
			if got := c.RateSource(); got != RateSourceDefault {
				t.Errorf("RateSource() = %q, 期望 %q", got, RateSourceDefault)
			}
		})
	}
}

// TestBaseCandidateRule 钉死 T38 的候选口径：已启用 且 小数位数 ≤ 2。
//
// 这是本包最核心的一条规则，两个判据缺一不可。
func TestBaseCandidateRule(t *testing.T) {
	t.Run("判据与实现逐项一致", func(t *testing.T) {
		for _, c := range All() {
			want := c.Enabled() && c.Scale() <= 2
			if got := c.CanBeBase(); got != want {
				t.Errorf("%s: CanBeBase() = %v, 期望 %v (enabled=%v, scale=%d)",
					c, got, want, c.Enabled(), c.Scale())
			}
		}
	})

	t.Run("3 位小数币种必须落选", func(t *testing.T) {
		kwd := MustParse("KWD")
		if kwd.Scale() != 3 {
			t.Fatalf("前置条件失败: KWD 的小数位数应为 3, 实际 %d", kwd.Scale())
		}
		if kwd.CanBeBase() {
			t.Error("KWD（3 位小数）不应是本位币候选")
		}
		for _, c := range BaseCandidates() {
			if c.String() == "KWD" {
				t.Error("BaseCandidates() 中不应含 KWD")
			}
		}
	})

	t.Run("0 位与 2 位币种入选", func(t *testing.T) {
		for _, code := range []string{"JPY", "KRW", "VND", "CNY", "USD"} {
			if !MustParse(code).CanBeBase() {
				t.Errorf("%s 应是本位币候选", code)
			}
		}
	})
}

// TestSpendCurrencyNotLimitedByBase 钉死承载 7：
// 「支出币种不受此限（3 位币种仍可正常记账与摊分）」。
//
// 「可作本位币」与「可作支出币种」是两个独立判据，不得合并。
func TestSpendCurrencyNotLimitedByBase(t *testing.T) {
	kwd, err := Parse("KWD")
	if err != nil {
		t.Fatalf("KWD 应能作为支出币种被解析: %v", err)
	}

	// 不能作本位币
	if kwd.CanBeBase() {
		t.Error("KWD 不应是本位币候选")
	}
	if _, err := ParseBase("KWD"); !errors.Is(err, ErrNotBaseCandidate) {
		t.Errorf("ParseBase(\"KWD\") 应返回 ErrNotBaseCandidate, 实际 %v", err)
	}

	// 但可以作支出币种：已启用、在 Enabled() 集合内、能取到摊分所需的小数位数
	if !kwd.Enabled() {
		t.Error("KWD 应处于启用状态，否则无法作为支出币种记账")
	}
	if kwd.Scale() != 3 {
		t.Errorf("KWD 的小数位数应为 3（8.2 摊分按此均分）, 实际 %d", kwd.Scale())
	}
	found := false
	for _, c := range Enabled() {
		if c.Equal(kwd) {
			found = true
			break
		}
	}
	if !found {
		t.Error("Enabled() 中应含 KWD——它是可选的支出币种")
	}
}

// TestEqualAndComparable 覆盖相等语义。
func TestEqualAndComparable(t *testing.T) {
	a := MustParse("CNY")
	b := MustParse("CNY")
	c := MustParse("USD")

	if !a.Equal(b) {
		t.Error("同一币种应相等")
	}
	if a.Equal(c) {
		t.Error("不同币种不应相等")
	}
	if a.Equal(Code{}) {
		t.Error("非零值不应与零值相等")
	}
}

// TestCodeIsComparable 钉住「Code 可比较」这条设计决定。
//
// 与 decimal.Decimal 刻意不可比较相反：币种是标称值，8.3 判重的四要素含
// 支出币种，== 在此是正确语义。此用例若编译不过即说明该性质被破坏。
func TestCodeIsComparable(t *testing.T) {
	a := MustParse("CNY")
	b := MustParse("CNY")

	if a != b { //nolint:staticcheck // 刻意用 == 验证可比较性
		t.Error("同一币种的 == 应为 true")
	}

	// 可作 map 键——store 的币种维度分组聚合（J-4）依赖这一点
	m := map[Code]int{a: 1}
	m[b]++
	if m[a] != 2 {
		t.Errorf("同一币种应命中同一 map 键, 实际 %d", m[a])
	}

	// 可作含币种字段的结构体的整体比较
	type pair struct {
		spend Code
		base  Code
	}
	p1 := pair{spend: MustParse("USD"), base: MustParse("CNY")}
	p2 := pair{spend: MustParse("USD"), base: MustParse("CNY")}
	if p1 != p2 {
		t.Error("含币种字段的结构体应可整体比较且相等")
	}
}

// TestCollectionsAreStableAndCopied 覆盖承载 6 对集合的两条要求：
// 顺序确定（下拉不能每次重排）、返回副本（调用方改动不污染包状态）。
func TestCollectionsAreStableAndCopied(t *testing.T) {
	t.Run("多次调用顺序一致", func(t *testing.T) {
		// map 遍历随机，若集合直接由 map 生成，这里会失败。
		// 跑 20 轮以降低随机顺序碰巧一致的概率。
		for i := 0; i < 20; i++ {
			assertSameOrder(t, All(), All(), "All")
			assertSameOrder(t, Enabled(), Enabled(), "Enabled")
			assertSameOrder(t, BaseCandidates(), BaseCandidates(), "BaseCandidates")
		}
	})

	t.Run("按代码字典序排列", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			list []Code
		}{
			{"All", All()},
			{"Enabled", Enabled()},
			{"BaseCandidates", BaseCandidates()},
		} {
			for i := 1; i < len(tc.list); i++ {
				if tc.list[i-1].String() >= tc.list[i].String() {
					t.Errorf("%s 未按字典序升序: %q 在 %q 之前",
						tc.name, tc.list[i-1], tc.list[i])
				}
			}
		}
	})

	t.Run("返回副本，改动不污染包状态", func(t *testing.T) {
		got := All()
		if len(got) == 0 {
			t.Fatal("All() 为空")
		}
		original := got[0]
		got[0] = Code{} // 篡改返回值

		if fresh := All(); !fresh[0].Equal(original) {
			t.Errorf("篡改返回切片影响了包状态: 重新取得 %q, 期望 %q", fresh[0], original)
		}
	})

	t.Run("NameKeys 同样返回副本且顺序确定", func(t *testing.T) {
		a, b := NameKeys(), NameKeys()
		if len(a) != len(b) {
			t.Fatalf("两次 NameKeys() 长度不同: %d vs %d", len(a), len(b))
		}
		for i := range a {
			if a[i] != b[i] {
				t.Errorf("NameKeys() 顺序不稳定: 第 %d 项 %q vs %q", i, a[i], b[i])
			}
		}
		if len(a) > 0 {
			a[0] = "篡改"
			if NameKeys()[0] == "篡改" {
				t.Error("篡改返回切片影响了包状态")
			}
		}
	})
}

// TestCollectionsAreConsistent 覆盖三个集合的收窄关系：
// BaseCandidates ⊆ Enabled ⊆ All。
func TestCollectionsAreConsistent(t *testing.T) {
	all := All()
	enabled := Enabled()
	candidates := BaseCandidates()

	if len(enabled) > len(all) {
		t.Errorf("Enabled(%d) 不应多于 All(%d)", len(enabled), len(all))
	}
	if len(candidates) > len(enabled) {
		t.Errorf("BaseCandidates(%d) 不应多于 Enabled(%d)", len(candidates), len(enabled))
	}

	inAll := make(map[Code]bool, len(all))
	for _, c := range all {
		inAll[c] = true
	}
	for _, c := range enabled {
		if !inAll[c] {
			t.Errorf("Enabled 中的 %q 不在 All 中", c)
		}
		if !c.Enabled() {
			t.Errorf("Enabled 中的 %q 实际未启用", c)
		}
	}
	inEnabled := make(map[Code]bool, len(enabled))
	for _, c := range enabled {
		inEnabled[c] = true
	}
	for _, c := range candidates {
		if !inEnabled[c] {
			t.Errorf("BaseCandidates 中的 %q 不在 Enabled 中", c)
		}
		if !c.CanBeBase() {
			t.Errorf("BaseCandidates 中的 %q 实际不满足候选口径", c)
		}
	}

	// NameKeys 与 All 一一对应
	keys := NameKeys()
	if len(keys) != len(all) {
		t.Errorf("NameKeys(%d) 与 All(%d) 数量不一致", len(keys), len(all))
	}
	for i, c := range all {
		if keys[i] != c.NameKey() {
			t.Errorf("第 %d 项文案键 %q 与 %q 不一致", i, keys[i], c.NameKey())
		}
	}
}

// TestDefaultBase 覆盖终版 I-2 的默认本位币。
func TestDefaultBase(t *testing.T) {
	base := DefaultBase()
	if base.String() != "CNY" {
		t.Errorf("DefaultBase() = %q, 期望 CNY（终版 I-2）", base)
	}
	// 默认值必须自身满足候选口径，否则新建账号即处于非法状态。
	if !base.CanBeBase() {
		t.Error("默认本位币必须满足本位币候选口径")
	}
}

// assertSameOrder 断言两个切片顺序一致。
func assertSameOrder(t *testing.T, a, b []Code, name string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("%s 两次调用长度不同: %d vs %d", name, len(a), len(b))
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			t.Fatalf("%s 顺序不稳定: 第 %d 项 %q vs %q", name, i, a[i], b[i])
		}
	}
}

// contains 报告 s 是否包含 sub。
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// BenchmarkParse 记录解析开销：它在每次请求的入参校验路径上。
func BenchmarkParse(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Parse("CNY"); err != nil {
			b.Fatal(err)
		}
	}
}
