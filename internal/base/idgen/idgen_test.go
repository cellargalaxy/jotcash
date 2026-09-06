package idgen

import (
	"strconv"
	"sync"
	"testing"
)

// 说明：本包的 ID 时间源在 go_common/util 内部硬编码为 time.Now()，不可注入，
// 因此用例一律只断言**可观测契约**（唯一 / 非零 / 严格递增 / 并发安全 / 宽度 /
// 5 个入口互不串号），不断言任何绝对时间——否则会造出依赖真实时钟推进的脆弱用例。

// allGens 5 个导出入口的集合，用于「逐入口」与「跨入口」两类用例统一取样。
// 键即实体名，断言失败时能直接定位到是哪个入口出的问题。
var allGens = map[string]func() int64{
	"审计ID": GenAuditID,
	"用户ID": GenUserID,
	"类型ID": GenCategoryID,
	"文件ID": GenFileID,
	"明细ID": GenExpenseID,
}

// TestGenUnique 顺序调用的唯一性：10 万次不得有任何碰撞。
// 唯一性是 5 类实体主键的根本要求，一旦碰撞即为「静默写坏数据」。
func TestGenUnique(t *testing.T) {
	const n = 100000
	seen := make(map[int64]struct{}, n)
	for i := 0; i < n; i++ {
		id := gen()
		if _, dup := seen[id]; dup {
			t.Fatalf("顺序生成出现碰撞，第 %d 次得到重复标识 %d", i+1, id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("唯一值数量与调用次数不等，期望 %d，实际 %d", n, len(seen))
	}
}

// TestGenNotZero 非零性：0 在本仓库是保留值（§四「零值 = 未设置」），
// 生成值一旦为 0 会与「未设置」混淆，故须逐个断言。
func TestGenNotZero(t *testing.T) {
	for i := 0; i < 100000; i++ {
		if id := gen(); id <= 0 {
			t.Fatalf("生成的标识非正数，第 %d 次得到 %d", i+1, id)
		}
	}
}

// TestGenMonotonic 严格递增：这是 store/sqlite 主键顺序写入（避免页分裂）的前提。
func TestGenMonotonic(t *testing.T) {
	const n = 100000
	prev := gen()
	for i := 1; i < n; i++ {
		id := gen()
		if id <= prev {
			t.Fatalf("标识非严格递增，第 %d 次：前一个 %d，当前 %d", i+1, prev, id)
		}
		prev = id
	}
}

// TestGenConcurrentUnique 并发唯一性：32 协程各取 3000 次，合计 9.6 万次不得碰撞。
// 配合 `go test -race` 可同时验证底层 GenId 的互斥量无数据竞争。
func TestGenConcurrentUnique(t *testing.T) {
	const goroutines, perGoroutine = 32, 3000

	var wg sync.WaitGroup
	// 每个协程先写入自己的局部切片，最后统一汇总，避免把锁竞争引入被测路径本身。
	buckets := make([][]int64, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			local := make([]int64, 0, perGoroutine)
			for i := 0; i < perGoroutine; i++ {
				local = append(local, gen())
			}
			buckets[g] = local
		}(g)
	}
	wg.Wait()

	total := goroutines * perGoroutine
	seen := make(map[int64]struct{}, total)
	for g := range buckets {
		for _, id := range buckets[g] {
			if id <= 0 {
				t.Fatalf("并发生成出现非正数标识 %d（协程 %d）", id, g)
			}
			if _, dup := seen[id]; dup {
				t.Fatalf("并发生成出现碰撞，重复标识 %d（协程 %d）", id, g)
			}
			seen[id] = struct{}{}
		}
	}
	if len(seen) != total {
		t.Fatalf("并发唯一值数量与调用次数不等，期望 %d，实际 %d（碰撞 %d）",
			total, len(seen), total-len(seen))
	}
}

// TestGenWidth 宽度与 int64 边界：GenId 产出「年份后两位 + 月日时分秒 + 微秒」共 18 位十进制，
// int64 上限为 19 位，故不会溢出。宽度一旦变化，说明底层 ID 格式已变，须重新评估
// answer 中登记的「JSON 须以字符串下发」结论。
func TestGenWidth(t *testing.T) {
	for i := 0; i < 1000; i++ {
		id := gen()
		if width := len(strconv.FormatInt(id, 10)); width != 18 {
			t.Fatalf("标识宽度非 18 位，得到 %d 位（id=%d）", width, id)
		}
	}
}

// TestEachEntryUnique 逐入口自查：每个入口单独连续取样也必须唯一且为正数。
func TestEachEntryUnique(t *testing.T) {
	const n = 5000
	for name, fn := range allGens {
		seen := make(map[int64]struct{}, n)
		for i := 0; i < n; i++ {
			id := fn()
			if id <= 0 {
				t.Fatalf("%s 生成非正数标识 %d", name, id)
			}
			if _, dup := seen[id]; dup {
				t.Fatalf("%s 出现碰撞，重复标识 %d", name, id)
			}
			seen[id] = struct{}{}
		}
	}
}

// TestCrossEntryNoCollision 跨入口不串号：5 个入口交叉取样，全域不得重复。
// 这是「同一 ID 空间、不同实体互不冲突」的直接验证——5 类实体共用一个生成器，
// 若跨入口出现重复，K-2 审计跳转就会把审计ID 指到别的实体上。
func TestCrossEntryNoCollision(t *testing.T) {
	const rounds = 20000
	seen := make(map[int64]string, rounds*len(allGens))
	for i := 0; i < rounds; i++ {
		for name, fn := range allGens {
			id := fn()
			if owner, dup := seen[id]; dup {
				t.Fatalf("跨入口出现碰撞：标识 %d 同时被 %s 与 %s 生成", id, owner, name)
			}
			seen[id] = name
		}
	}
}

// TestExportedEntriesDelegateGen 契约核对：5 个导出入口都必须真正产出标识（非零值），
// 防止将来重构时某个入口被写成空实现或忘记返回。
func TestExportedEntriesDelegateGen(t *testing.T) {
	if got := len(allGens); got != 5 {
		t.Fatalf("导出入口数量应为 5（对应 5 类实体与 5 个创建方），实际 %d", got)
	}
	for name, fn := range allGens {
		if id := fn(); id <= 0 {
			t.Fatalf("%s 入口未产出有效标识，得到 %d", name, id)
		}
	}
}

// BenchmarkGen 生成开销基准，供将来换方案时对比。
func BenchmarkGen(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = gen()
	}
}
