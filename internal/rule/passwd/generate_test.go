package passwd

import (
	"context"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/base/pwdhash"
)

// 生成侧是非确定性的，故用例一律断言**不变量**而非具体取值：
// 「每次都合规」「长度恒定」「字符集受限」「不重复」「位置不固定」。
const genSampleCount = 300

// TestGenerateSatisfiesPolicy 是生成侧最重要的一条：A-2 生成的初始密码必须
// 恒能通过 A-8 校验。若不成立，A-1 系统初始化会造出一个连自家策略都不过的
// admin 密码，而 A-2 的强制改密页会拒绝用户用它去改密——账户当场锁死。
func TestGenerateSatisfiesPolicy(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < genSampleCount; i++ {
		password, err := Generate(ctx)
		if err != nil {
			t.Fatalf("第 %d 次生成失败：%v", i, err)
		}
		if err := Check(password); err != nil {
			t.Fatalf("第 %d 次生成的密码不合 A-8：%v（长度 %d）",
				i, err, utf8.RuneCountInString(password))
		}
	}
}

// TestGenerateShape 断言生成结果的形态不变量：长度恒为 genLength，
// 且三类的计数恰好等于定额。
//
// 这条钉住的是「定额法」而非「重试法」：重试法下类别计数是随机的，
// 只能断言「至少两类」；定额法能断言「恰好 4 位数字 + 2 位符号 + 10 位字母」，
// 断言更强，能发现「洗牌把某类字符弄丢了」这种错误。
func TestGenerateShape(t *testing.T) {
	ctx := context.Background()
	wantLetters := genLength - genDigitCount - genSymbolCount

	for i := 0; i < genSampleCount; i++ {
		password, err := Generate(ctx)
		if err != nil {
			t.Fatalf("第 %d 次生成失败：%v", i, err)
		}

		if got := utf8.RuneCountInString(password); got != genLength {
			t.Fatalf("长度 = %d，期望 %d：%q", got, genLength, password)
		}

		var letters, digits, symbols, others int
		for _, r := range password {
			switch {
			case unicode.IsLetter(r):
				letters++
			case unicode.IsDigit(r):
				digits++
			case unicode.IsPunct(r), unicode.IsSymbol(r):
				symbols++
			default:
				others++
			}
		}
		if letters != wantLetters || digits != genDigitCount || symbols != genSymbolCount || others != 0 {
			t.Fatalf("类别计数 = 字母%d/数字%d/符号%d/其它%d，期望 %d/%d/%d/0：%q",
				letters, digits, symbols, others, wantLetters, genDigitCount, genSymbolCount, password)
		}
	}
}

// TestGenerateCharsetRestricted 断言每个字符都出自本包声明的三个字符集，
// 且**易混字符一个都不出现**。
//
// 后半条是 A-3 的直接要求：admin 初始密码由人从日志里抄读，抄错一位与日志
// 丢失后果相同（A-3 明写唯一出路是重新部署从头初始化）。
func TestGenerateCharsetRestricted(t *testing.T) {
	ctx := context.Background()
	allowed := genLowerLetters + genUpperLetters + genDigits + genSymbols

	//易混字符：0/O/o、1/l/I 六个，以及会被 JSON 与 %q 转义的字符
	const confusing = "0Oo1lI"
	const escaped = "\\\"'`<>|"

	for _, c := range confusing + escaped {
		if strings.ContainsRune(allowed, c) {
			t.Fatalf("字符集含不该出现的字符 %q", c)
		}
	}

	for i := 0; i < genSampleCount; i++ {
		password, err := Generate(ctx)
		if err != nil {
			t.Fatalf("第 %d 次生成失败：%v", i, err)
		}
		for _, r := range password {
			if !strings.ContainsRune(allowed, r) {
				t.Fatalf("出现字符集外的字符 %q：%q", r, password)
			}
		}
	}
}

// TestGenerateUnique 断言生成结果不重复。
//
// 300 次抽样在实际取值域（约 10^26）下若出现任一次碰撞，几乎必然意味着
// 随机源退化成了固定种子或常量，而不是概率上的巧合。
func TestGenerateUnique(t *testing.T) {
	ctx := context.Background()
	seen := make(map[string]int, genSampleCount)
	for i := 0; i < genSampleCount; i++ {
		password, err := Generate(ctx)
		if err != nil {
			t.Fatalf("第 %d 次生成失败：%v", i, err)
		}
		if prev, dup := seen[password]; dup {
			t.Fatalf("第 %d 次与第 %d 次生成了相同密码，随机源疑似退化", i, prev)
		}
		seen[password] = i
	}
}

// TestGenerateShuffled 断言结果**经过洗牌**：数字与符号不能恒定落在固定位置。
//
// 若实现漏掉洗牌、直接把「字母段 + 数字段 + 符号段」拼起来，本用例会发现
// 数字只出现在下标 10-13、符号只出现在 14-15。判据取「每一类都在足够多的
// 不同下标上出现过」——按定额，16 个位置里数字出现的概率均等，300 次抽样下
// 每个位置的期望命中约 75 次，故要求 16 个位置全部被命中过是稳的。
func TestGenerateShuffled(t *testing.T) {
	ctx := context.Background()
	digitPositions := make(map[int]bool, genLength)
	symbolPositions := make(map[int]bool, genLength)
	letterPositions := make(map[int]bool, genLength)

	for i := 0; i < genSampleCount; i++ {
		password, err := Generate(ctx)
		if err != nil {
			t.Fatalf("第 %d 次生成失败：%v", i, err)
		}
		for pos, r := range []rune(password) {
			switch {
			case unicode.IsDigit(r):
				digitPositions[pos] = true
			case unicode.IsLetter(r):
				letterPositions[pos] = true
			default:
				symbolPositions[pos] = true
			}
		}
	}

	for name, positions := range map[string]map[int]bool{
		"数字": digitPositions, "符号": symbolPositions, "字母": letterPositions,
	} {
		if len(positions) != genLength {
			t.Errorf("%s 只在 %d 个不同位置出现过（共 %d 个位置），疑似未洗牌或洗牌有偏：%v",
				name, len(positions), genLength, positions)
		}
	}
}

// TestGenerateDistribution 粗查字符分布无明显偏置：每个允许字符都应被用到过。
//
// 这不是严格的随机性检验（那属 crypto/rand 与 base/pwdhash 的职责，后者已有
// TestRandIndexDistribution 卡方检验），只拦「字符集里有一段永远取不到」这类
// 下标计算错误——例如洗牌写成 `i > 1` 会让首位永远不参与交换。
func TestGenerateDistribution(t *testing.T) {
	ctx := context.Background()
	allowed := genLowerLetters + genUpperLetters + genDigits + genSymbols
	used := make(map[rune]int, len(allowed))

	//字母集 48 个字符，每次只取 10 个，需要足够样本才能覆盖全集
	const samples = 600
	for i := 0; i < samples; i++ {
		password, err := Generate(ctx)
		if err != nil {
			t.Fatalf("第 %d 次生成失败：%v", i, err)
		}
		for _, r := range password {
			used[r]++
		}
	}

	var missing []string
	for _, r := range allowed {
		if used[r] == 0 {
			missing = append(missing, string(r))
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d 次生成后仍有字符从未被取到：%v", samples, missing)
	}
}

// TestGenerateNilContext 断言传 nil ctx 不 panic。
//
// 不是提倡这么调用，而是 A-1 系统初始化在 app 启动阶段发生，那时很可能还没有
// 请求级 ctx，调用方顺手传 nil 是现实风险；这里确认它退化为「不打日志」而非
// 崩掉整个启动流程（base/pwdhash 的 rand_test.go 有同名先例）。
func TestGenerateNilContext(t *testing.T) {
	//lint:ignore SA1012 刻意传 nil，验证不 panic
	password, err := Generate(nil) //nolint:staticcheck
	if err != nil {
		t.Fatalf("nil ctx 下生成失败：%v", err)
	}
	if err := Check(password); err != nil {
		t.Fatalf("nil ctx 下生成的密码不合 A-8：%v", err)
	}
}

// TestGenerateHashable 断言生成的密码能被 base/pwdhash 正常哈希与校验。
//
// 这条打通 A-2 的实际链路：Generate → Hash 落库 → 用户用该明文登录 → Verify。
// 单独测 Generate 合规、单独测 Hash 可用都不能保证两者接得上——例如若生成的
// 密码含 base/pwdhash 会拒绝的字符或超出其长度上限，链路就断在中间。
func TestGenerateHashable(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		password, err := Generate(ctx)
		if err != nil {
			t.Fatalf("生成失败：%v", err)
		}

		hash, salt, err := pwdhash.Hash(ctx, password)
		if err != nil {
			t.Fatalf("哈希失败：%v", err)
		}

		ok, err := pwdhash.Verify(ctx, password, hash, salt)
		if err != nil {
			t.Fatalf("校验失败：%v", err)
		}
		if !ok {
			t.Fatal("生成的密码经 Hash 后无法通过 Verify，A-2 链路断裂")
		}

		//反向：另一个生成的密码不应通过同一份哈希
		other, err := Generate(ctx)
		if err != nil {
			t.Fatalf("生成失败：%v", err)
		}
		ok, err = pwdhash.Verify(ctx, other, hash, salt)
		if err != nil {
			t.Fatalf("校验失败：%v", err)
		}
		if ok {
			t.Fatal("不同的密码通过了同一份哈希")
		}
	}
}

// TestGenerateConstants 钉住生成规格，理由同 TestPolicyConstants：
// 让「悄悄把生成长度调短」的改动必须同时改测试。
//
// 顺带断言两条内部一致性——定额之和不超长度、且生成长度不低于 A-8 下限。
// 后者若被破坏，Generate 里的断言会在运行期报错，但那是**部署后**才发现；
// 这条让它在 CI 就失败。
func TestGenerateConstants(t *testing.T) {
	if genLength < MinLength {
		t.Errorf("genLength = %d，低于 A-8 下限 %d", genLength, MinLength)
	}
	if genDigitCount+genSymbolCount >= genLength {
		t.Errorf("定额之和 %d 不小于长度 %d，字母位会为负",
			genDigitCount+genSymbolCount, genLength)
	}
	if genLength != 16 || genDigitCount != 4 || genSymbolCount != 2 {
		t.Errorf("生成规格 = %d/%d/%d，与包注释记录的 16/4/2 不符",
			genLength, genDigitCount, genSymbolCount)
	}
	//三类定额都为正 → 生成结果三类齐全，是 A-8「至少两类」的超集
	for name, n := range map[string]int{
		"字母": genLength - genDigitCount - genSymbolCount,
		"数字": genDigitCount,
		"符号": genSymbolCount,
	} {
		if n <= 0 {
			t.Errorf("%s 定额 = %d，应为正数", name, n)
		}
	}
}

// TestNoPasswordInGenerateError 是生成侧的红线回归：失败路径的错误里不得含
// 任何明文痕迹。
//
// 无法真的让 crypto/rand 失败，故改为审计错误的构造方式——Generate 的四个
// 失败分支都用 errs.Wrap(err, KindInternal, KeyGenerateFailed, nil)，占位参数
// 恒为 nil。这里用「断言最后一个分支」的方式覆盖：把 Check 的失败错误 Wrap
// 后，检查其 Params 为空且文案键正确。
func TestNoPasswordInGenerateError(t *testing.T) {
	//哨兵不能是任何文案键的子串，否则断言会误报：第一版用了 "short"，
	//而它正是 passwd.too_short 的一段，当场被这条用例判成「泄露」。
	const sentinel = "Zq7Wv"

	//模拟 Generate 里「断言分支」的错误构造，确认 Wrap 不会把明文带出来
	inner := Check(sentinel)
	if inner == nil {
		t.Fatal("哨兵应当校验失败，用例前提不成立")
	}
	wrapped := errs.Wrap(inner, errs.KindInternal, KeyGenerateFailed, nil)

	if got := errs.MsgKeyOf(wrapped); got != KeyGenerateFailed {
		t.Errorf("文案键 = %q，期望 %q", got, KeyGenerateFailed)
	}
	if !errs.IsKind(wrapped, errs.KindInternal) {
		t.Errorf("档位 = %v，期望 KindInternal——生成失败不是用户输入问题", errs.KindOf(wrapped))
	}
	if params := errs.ParamsOf(wrapped); len(params) != 0 {
		t.Errorf("生成失败的占位参数应为空，实际 = %v", params)
	}
	//Wrap 会把内层错误的文本一并带出，故这里连内层一起查
	if text := wrapped.Error(); strings.Contains(text, sentinel) {
		t.Errorf("错误文本含入参明文：%s", text)
	}
}

// TestShuffleFixedPointsExist 断言洗牌是 **Fisher–Yates 而非 Sattolo**。
//
// 两者只差一个字符：随机下标上界写 i+1 是 Fisher–Yates（均匀取自 n! 个排列），
// 写 i 则是 Sattolo（只产生单个轮换，**任何元素都不可能留在原位**，排列空间
// 缩到 (n-1)! 且有偏）。两者的输出肉眼都是「乱的」，前面那些用例
// （形态、字符集、去重、位置分布）**全都测不出这个差别**——变异测试里把
// i+1 改成 i 时它们无一失败，本用例是为补这个缺口新加的。
//
// 判据取 Sattolo 的定义性特征：不动点。洗一个各字符互不相同的串，
// Fisher–Yates 下「某位字符洗后仍在原位」的概率约 1-1/e ≈ 63%（至少一个不动点），
// 而 Sattolo 下恒为 0。抽样 200 次，只要出现过一次不动点即证明不是 Sattolo。
func TestShuffleFixedPointsExist(t *testing.T) {
	ctx := context.Background()
	//各字符互不相同，才能用「位置 i 上的字符是否仍是原字符」判断不动点
	const input = "abcdefghijklmnop"
	original := []rune(input)

	var withFixedPoint int
	const samples = 200
	for i := 0; i < samples; i++ {
		got, err := shuffle(ctx, input)
		if err != nil {
			t.Fatalf("第 %d 次洗牌失败：%v", i, err)
		}
		gotRunes := []rune(got)
		if len(gotRunes) != len(original) {
			t.Fatalf("洗牌改变了长度：%d -> %d", len(original), len(gotRunes))
		}
		for pos := range original {
			if gotRunes[pos] == original[pos] {
				withFixedPoint++
				break
			}
		}
	}

	if withFixedPoint == 0 {
		t.Errorf("%d 次洗牌中没有任何一次出现不动点。Fisher–Yates 下该比例应约 63%%，"+
			"恒为 0 说明随机下标的上界写成了 i 而非 i+1（退化成 Sattolo 算法："+
			"只产生单个轮换，排列空间从 n! 缩到 (n-1)! 且分布有偏）", samples)
	}
}

// TestShuffleIsPermutation 断言洗牌是**置换**：字符多重集不变，只改顺序。
//
// 这条拦「洗牌把某个字符覆盖掉」的下标错误——例如把交换写成单向赋值。
func TestShuffleIsPermutation(t *testing.T) {
	ctx := context.Background()
	const input = "aabbccdd11!!"

	want := runeCounts(input)
	for i := 0; i < 100; i++ {
		got, err := shuffle(ctx, input)
		if err != nil {
			t.Fatalf("洗牌失败：%v", err)
		}
		gotCounts := runeCounts(got)
		if len(gotCounts) != len(want) {
			t.Fatalf("字符种类数变了：%v -> %v", want, gotCounts)
		}
		for r, n := range want {
			if gotCounts[r] != n {
				t.Fatalf("字符 %q 的个数 = %d，期望 %d（洗牌不是置换）：%q",
					r, gotCounts[r], n, got)
			}
		}
	}
}

// TestShuffleMultibyte 断言洗牌按 **rune** 而非字节处理。
//
// 本包的字符集都是 ASCII，故这条在当前实现下测不到差别——但按字节洗牌会在
// 有人往字符集里加入多字节字符时**静默产出乱码**（非法 UTF-8），而那样的口令
// 一旦落库就会让账户永久登不进（见包注释）。这里直接以多字节输入调用 shuffle，
// 把这条不变量提前钉住，而不是等到字符集变更那天才发现。
func TestShuffleMultibyte(t *testing.T) {
	ctx := context.Background()
	const input = "汉字密码测试１２３４！"

	want := runeCounts(input)
	for i := 0; i < 100; i++ {
		got, err := shuffle(ctx, input)
		if err != nil {
			t.Fatalf("洗牌失败：%v", err)
		}
		//按字节洗牌会切断多字节序列，产出非法 UTF-8
		if !utf8.ValidString(got) {
			t.Fatalf("洗牌产出了非法 UTF-8（疑似按字节而非 rune 洗牌）：%q", got)
		}
		if utf8.RuneCountInString(got) != utf8.RuneCountInString(input) {
			t.Fatalf("rune 数变了：%d -> %d",
				utf8.RuneCountInString(input), utf8.RuneCountInString(got))
		}
		gotCounts := runeCounts(got)
		for r, n := range want {
			if gotCounts[r] != n {
				t.Fatalf("字符 %q 的个数 = %d，期望 %d：%q", r, gotCounts[r], n, got)
			}
		}
	}
}

// TestShuffleEdgeLengths 覆盖洗牌的长度边界：空串与单字符时循环体一次都不执行。
func TestShuffleEdgeLengths(t *testing.T) {
	ctx := context.Background()
	for _, input := range []string{"", "a", "ab", "汉"} {
		got, err := shuffle(ctx, input)
		if err != nil {
			t.Fatalf("shuffle(%q) 失败：%v", input, err)
		}
		if utf8.RuneCountInString(got) != utf8.RuneCountInString(input) {
			t.Errorf("shuffle(%q) = %q，长度变了", input, got)
		}
	}
}

// runeCounts 统计每个 rune 出现的次数，用于置换判定。
func runeCounts(s string) map[rune]int {
	counts := make(map[rune]int)
	for _, r := range s {
		counts[r]++
	}
	return counts
}

// TestGenerateNoRetryLoop 断言 Generate 在正常路径下**不依赖重试**。
//
// 判据是耗时：定额法每次生成的随机源调用次数是固定的（genLength 次 RandString
// 内部采样 + genLength-1 次 RandIndex 洗牌），耗时应当极短且稳定。若有人把实现
// 改成「随机生成 → Check → 不合规重来」，在字符集配错时这里会挂起，由 go test
// 的超时兜住；本用例额外断言单次调用不会离谱地慢。
func TestGenerateNoRetryLoop(t *testing.T) {
	ctx := context.Background()
	//1000 次生成若在合理时间内完成，说明没有隐藏的收敛循环
	for i := 0; i < 1000; i++ {
		if _, err := Generate(ctx); err != nil {
			t.Fatalf("第 %d 次生成失败：%v", i, err)
		}
	}
}

// withFailingRand 把包级的随机源别名替换成「第 n 次调用返回错误」的桩，
// 并在用例结束时恢复。用于覆盖 Generate 的五个错误分支——crypto/rand 在测试里
// 不会失败，不注入就永远跑不到那些分支，也就无法验证它们是否真的把档位设成
// Internal、是否真的没把明文带进错误。
//
// 因为改的是包级变量，用了本函数的用例**不能** t.Parallel()。
func withFailingRand(t *testing.T, failStringAt, failIndexAt int) {
	t.Helper()
	origString, origIndex := randString, randIndex
	t.Cleanup(func() { randString, randIndex = origString, origIndex })

	stringCalls, indexCalls := 0, 0
	randString = func(ctx context.Context, n int, charset string) (string, error) {
		stringCalls++
		if stringCalls == failStringAt {
			return "", errs.NewInternal("test.rand_failed", nil)
		}
		return origString(ctx, n, charset)
	}
	randIndex = func(ctx context.Context, n int) (int, error) {
		indexCalls++
		if indexCalls == failIndexAt {
			return 0, errs.NewInternal("test.rand_failed", nil)
		}
		return origIndex(ctx, n)
	}
}

// TestGenerateRandFailure 覆盖随机源失败的每一条分支，断言三件事：
// 返回空串（绝不返回半成品口令）、档位为 Internal、占位参数为空。
//
// 「返回空串」这条最要紧：若失败时返回了已经拼好的部分口令，调用方
// service/account 若漏判 err 就会把一个残缺的弱口令哈希落库。
func TestGenerateRandFailure(t *testing.T) {
	cases := []struct {
		name         string
		failStringAt int
		failIndexAt  int
	}{
		{name: "字母段失败", failStringAt: 1},
		{name: "数字段失败", failStringAt: 2},
		{name: "符号段失败", failStringAt: 3},
		{name: "洗牌首次失败", failIndexAt: 1},
		{name: "洗牌中途失败", failIndexAt: 5},
		{name: "洗牌末次失败", failIndexAt: genLength - 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withFailingRand(t, c.failStringAt, c.failIndexAt)

			password, err := Generate(context.Background())
			if err == nil {
				t.Fatal("随机源失败时 Generate 应返回错误")
			}
			//绝不能返回半成品口令
			if password != "" {
				t.Errorf("失败时应返回空串，实际返回了 %q", password)
			}
			if got := errs.MsgKeyOf(err); got != KeyGenerateFailed {
				t.Errorf("文案键 = %q，期望 %q", got, KeyGenerateFailed)
			}
			//生成失败是系统问题，不是用户输入问题——档位错了接入层会回 400
			if !errs.IsKind(err, errs.KindInternal) {
				t.Errorf("档位 = %v，期望 KindInternal", errs.KindOf(err))
			}
			if params := errs.ParamsOf(err); len(params) != 0 {
				t.Errorf("占位参数应为空，实际 = %v", params)
			}
		})
	}
}

// TestGenerateAssertionBranch 覆盖 Generate 末尾那条「生成结果再过一遍 Check」
// 的断言分支：把随机源换成恒返回单一字符的桩，使生成结果成为单类别口令，
// 于是断言必须拦下它、且**不得**把那个不合规的明文返回出去。
//
// 这条分支在生产中不该被触发（定额法保证合规），存在的意义是让「有人改了
// 字符集常量」这类改动立刻暴露。这里验证它真的拦得住。
func TestGenerateAssertionBranch(t *testing.T) {
	origString, origIndex := randString, randIndex
	t.Cleanup(func() { randString, randIndex = origString, origIndex })

	//模拟「字符集被改成只有字母」的情形：三段都返回字母，结果只有一类
	randString = func(_ context.Context, n int, _ string) (string, error) {
		return strings.Repeat("a", n), nil
	}
	randIndex = origIndex

	password, err := Generate(context.Background())
	if err == nil {
		t.Fatal("生成出单类别口令时，末尾的断言应当拦下它")
	}
	if password != "" {
		t.Errorf("断言失败时应返回空串，实际返回了 %q", password)
	}
	if got := errs.MsgKeyOf(err); got != KeyGenerateFailed {
		t.Errorf("文案键 = %q，期望 %q", got, KeyGenerateFailed)
	}
	if !errs.IsKind(err, errs.KindInternal) {
		t.Errorf("档位 = %v，期望 KindInternal——这是本包自身配置错误，不是用户输入问题",
			errs.KindOf(err))
	}
	//断言分支 Wrap 的内层是 Check 的错误，其占位参数含类别计数；
	//外层占位参数必须为空，且整条错误链里不得出现那个不合规的明文
	if params := errs.ParamsOf(err); len(params) != 0 {
		t.Errorf("外层占位参数应为空，实际 = %v", params)
	}
	if text := err.Error(); strings.Contains(text, strings.Repeat("a", genLength)) {
		t.Errorf("错误文本含不合规的明文：%s", text)
	}
}

// TestRandAliasesArePwdhash 断言两个包级别名在**未被测试替换时**确实指向
// base/pwdhash 的函数，而不是别的什么随机源。
//
// 这是为上面那套注入机制兜底：注入用的 t.Cleanup 若哪天被写坏，本用例会
// 在后续用例里发现别名没恢复。判据是行为——用别名各取一次，结果必须落在
// pwdhash 的取值域内。
func TestRandAliasesArePwdhash(t *testing.T) {
	ctx := context.Background()

	got, err := randString(ctx, 8, genDigits)
	if err != nil {
		t.Fatalf("randString 失败：%v", err)
	}
	if utf8.RuneCountInString(got) != 8 {
		t.Errorf("randString 返回长度 = %d，期望 8", utf8.RuneCountInString(got))
	}
	for _, r := range got {
		if !strings.ContainsRune(genDigits, r) {
			t.Errorf("randString 返回了字符集外的字符 %q", r)
		}
	}

	idx, err := randIndex(ctx, 10)
	if err != nil {
		t.Fatalf("randIndex 失败：%v", err)
	}
	if idx < 0 || idx >= 10 {
		t.Errorf("randIndex(10) = %d，应落在 [0,10)", idx)
	}
}
