package idgen

import (
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestGenPositive 钉住本包最核心的一条性质：ID 恒为正整数。
//
// 「零值 = 无 ID」这条语义被 AuditLog.操作对象类型/对象ID（终版 §四「零值 = 本次
// 操作无具体对象或为批量操作」）与 Expense.来源文件ID（「手工录入时为空」）依赖，
// 一旦发出 0 或负值，有主记录会被静默展示成「无具体对象」。
func TestGenPositive(t *testing.T) {
	for i := 0; i < 10000; i++ {
		if id := gen(); id <= 0 {
			t.Fatalf("第 %d 次生成得到非正数 %d", i, id)
		}
	}
}

// TestGenUnique 顺序唯一性：同一微秒内的连续调用不得重号。
//
// go_common 的 GenId 只精确到微秒，而 time.Now 的实测步进约几十纳秒，
// 其内部靠「上次微秒时刻 +1」保证唯一。本测试是对该机制的回归断言——
// 若上游退化为裸时间编码，重复率会立刻显现（其源码注释记录了未修复前的
// 实测重复率约 86%）。
func TestGenUnique(t *testing.T) {
	const n = 200000
	seen := make(map[int64]struct{}, n)
	for i := 0; i < n; i++ {
		id := gen()
		if _, dup := seen[id]; dup {
			t.Fatalf("第 %d 次生成出现重复 ID %d", i, id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != n {
		t.Errorf("唯一 ID 数 = %d, 期望 %d", len(seen), n)
	}
}

// TestGenMonotonic 单调递增：store 的游标迭代（I-7 与 K-3 不全量装载）
// 用「ID > 上次游标」作稳定游标，依赖这条性质。
func TestGenMonotonic(t *testing.T) {
	const n = 50000
	prev := gen()
	for i := 1; i < n; i++ {
		id := gen()
		if id <= prev {
			t.Fatalf("第 %d 次生成的 %d 未大于前一个 %d，游标迭代会漏行或重复", i, id, prev)
		}
		prev = id
	}
}

// TestGenConcurrentUnique 并发唯一性：审计与明细的 ID 在并发请求下不得撞号。
// 撞号会让不变式 4 的「一次提交 = 一条审计 = 一个批次」失去边界。
func TestGenConcurrentUnique(t *testing.T) {
	const goroutines, per = 64, 3000
	var (
		mu   sync.Mutex
		seen = make(map[int64]struct{}, goroutines*per)
		wg   sync.WaitGroup
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 先本地攒齐再统一入表，避免锁竞争掩盖真实的并发发号行为
			local := make([]int64, 0, per)
			for j := 0; j < per; j++ {
				local = append(local, gen())
			}
			mu.Lock()
			defer mu.Unlock()
			for _, id := range local {
				seen[id] = struct{}{}
			}
		}()
	}
	wg.Wait()

	total := goroutines * per
	if len(seen) != total {
		t.Errorf("并发生成 %d 个 ID，唯一值仅 %d 个，重复 %d 个", total, len(seen), total-len(seen))
	}
}

// TestFiveEntryPointsShareSpace 五个具名入口必须共用同一取值空间。
//
// AuditLog.对象ID 是跨实体存放的历史指针（终版 §七「对象ID 是历史指针，
// 不是外键」），若五类 ID 各有独立空间，同一个数值可能既是类型ID 又是文件ID，
// K-2 跳转就无从判断该跳向哪个实体。
func TestFiveEntryPointsShareSpace(t *testing.T) {
	gens := map[string]func() int64{
		"GenAuditID":    GenAuditID,
		"GenUserID":     GenUserID,
		"GenCategoryID": GenCategoryID,
		"GenFileID":     GenFileID,
		"GenExpenseID":  GenExpenseID,
	}

	const per = 2000
	seen := make(map[int64]string, len(gens)*per)
	for name, fn := range gens {
		for i := 0; i < per; i++ {
			id := fn()
			if id <= 0 {
				t.Fatalf("%s 生成非正数 %d", name, id)
			}
			if owner, dup := seen[id]; dup {
				t.Fatalf("%s 生成的 %d 与 %s 撞号，五类 ID 取值空间未统一", name, id, owner)
			}
			seen[id] = name
		}
	}
	if len(seen) != len(gens)*per {
		t.Errorf("五入口共生成 %d 个唯一 ID, 期望 %d", len(seen), len(gens)*per)
	}
}

// TestIsValid IsValid 是「零值 = 无 ID」的可判定形式。
func TestIsValid(t *testing.T) {
	cases := []struct {
		id   int64
		want bool
		desc string
	}{
		{0, false, "零值 = 无 ID（AuditLog 批量操作、手工录入无来源文件）"},
		{-1, false, "负数非法"},
		{1, true, "最小正数"},
		{260906160443337625, true, "实测形态的 18 位 ID"},
		{9223372036854775807, true, "int64 上界"},
	}
	for _, c := range cases {
		if got := IsValid(c.id); got != c.want {
			t.Errorf("IsValid(%d) = %v, 期望 %v（%s）", c.id, got, c.want, c.desc)
		}
	}
	// 本包发出的 ID 必须全部通过 IsValid
	for i := 0; i < 1000; i++ {
		if id := gen(); !IsValid(id) {
			t.Fatalf("本包发出的 %d 未通过 IsValid", id)
		}
	}
}

// TestStringAndParseStringRoundTrip String 与 ParseString 必须严格互逆。
// dto 层依赖这一对函数在 JSON 字符串与 int64 之间无损转换。
func TestStringAndParseStringRoundTrip(t *testing.T) {
	for i := 0; i < 5000; i++ {
		id := gen()
		s := String(id)
		back, ok := ParseString(s)
		if !ok {
			t.Fatalf("ParseString(%q) 失败，而该串来自 String(%d)", s, id)
		}
		if back != id {
			t.Fatalf("往返不一致: %d -> %q -> %d", id, s, back)
		}
	}

	// 零值的文本形态固定为 "0"
	if got := String(0); got != "0" {
		t.Errorf("String(0) = %q, 期望 \"0\"", got)
	}
}

// TestParseStringRejectsIllegal ParseString 必须显式拒绝非法输入，且失败时 id 恒为 0。
//
// 这是与 go_common 的 util.String2Int 的关键差异：后者在失败与溢出时返回钳制值
// （0 / MaxInt64 / MinInt64）并吞掉 error，非法输入会静默变成一个看似合法的 ID。
// 归属校验（不变式 3）的入口不能建立在这种语义上。
func TestParseStringRejectsIllegal(t *testing.T) {
	illegal := []struct{ in, desc string }{
		{"", "空串"},
		{" ", "空白"},
		{" 123", "前导空白（机器生成的串不该有空白，出现即调用方拼装有误）"},
		{"123 ", "尾随空白"},
		{"0", "零值不是合法 ID（零值语义是「无 ID」，不是某条记录）"},
		{"-1", "负数"},
		{"-260906160443337625", "负号 + 合法形态"},
		{"+123", "显式正号"},
		{"12.3", "小数"},
		{"1e5", "科学计数法"},
		{"abc", "非数字"},
		{"260906160443337625x", "尾部杂字符"},
		{"0x10", "十六进制"},
		{"007", "前导零（本包发出的 ID 无前导零）"},
		{"9223372036854775808", "溢出 int64 上界 1"},
		{"18446744073709551615", "MaxUint64——util.String2Int 会把它钳成 MaxInt64"},
		{"99999999999999999999999", "远超 int64"},
	}
	for _, c := range illegal {
		id, ok := ParseString(c.in)
		if ok {
			t.Errorf("ParseString(%q) 应失败却成功，得 %d（%s）", c.in, id, c.desc)
		}
		if id != 0 {
			t.Errorf("ParseString(%q) 失败时应返回 0，实际 %d（%s）——不得返回钳制值", c.in, id, c.desc)
		}
	}
}

// TestParseStringNoClampSemantics 溢出时不得沿用钳制语义。
// 若把溢出串钳成 MaxInt64，归属校验会拿一个不存在的合法 ID 去查库，
// 越权判定的入口就被一个非法输入绕开了。
func TestParseStringNoClampSemantics(t *testing.T) {
	const overflow = "9223372036854775808" // MaxInt64 + 1
	id, ok := ParseString(overflow)
	if ok || id != 0 {
		t.Errorf("ParseString(%q) = (%d, %v), 期望 (0, false)", overflow, id, ok)
	}
	// 对照：MaxInt64 本身是合法形态，必须放行
	if id, ok := ParseString("9223372036854775807"); !ok || id != 9223372036854775807 {
		t.Errorf("ParseString(MaxInt64) = (%d, %v), 期望 (9223372036854775807, true)", id, ok)
	}
}

// TestMustPositivePanics 断言守卫本身有效。
//
// gen 依赖 mustPositive 在「上游发出非正值」时立刻 panic，而不是让错 ID 流入
// 数据库。该分支无法靠改系统时钟触发，故直接对 mustPositive 施加输入。
func TestMustPositivePanics(t *testing.T) {
	for _, bad := range []int64{0, -1, -260906160443337625, -9223372036854775808} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("mustPositive(%d) 应 panic 却正常返回", bad)
					return
				}
				msg, ok := r.(string)
				if !ok || msg == "" {
					t.Errorf("mustPositive(%d) 的 panic 值 = %v, 期望非空字符串", bad, r)
					return
				}
				// 报错须带上实际值，便于定位环境级故障
				if !strings.Contains(msg, strconv.FormatInt(bad, 10)) {
					t.Errorf("panic 信息 %q 未包含实际值 %d", msg, bad)
				}
			}()
			_ = mustPositive(bad)
		}()
	}

	// 正数必须原样透出，不得篡改
	for _, good := range []int64{1, 260906160443337625, 9223372036854775807} {
		if got := mustPositive(good); got != good {
			t.Errorf("mustPositive(%d) = %d, 期望原样返回", good, got)
		}
	}
}

// TestGenDigitsWithinInt64 ID 位数须在 int64 承载范围内且有充足余量。
func TestGenDigitsWithinInt64(t *testing.T) {
	id := gen()
	digits := len(strconv.FormatInt(id, 10))
	// 实测常规年份（2010~2099）恒为 18 位；这里放宽到 15~19 位，
	// 只断言「不接近 int64 上界（19 位且首位大）」这一底线。
	if digits < 15 || digits > 19 {
		t.Errorf("ID 位数 = %d（id=%d），超出预期的 15~19 位", digits, id)
	}
	// 距上界的余量：实测约 35 倍
	const int64Max = int64(9223372036854775807)
	if headroom := float64(int64Max) / float64(id); headroom < 2 {
		t.Errorf("ID %d 距 int64 上界余量仅 %.2f 倍，存在溢出风险", id, headroom)
	}
}
