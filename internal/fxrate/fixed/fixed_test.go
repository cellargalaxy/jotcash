package fixed

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/fxrate"
	"github.com/sirupsen/logrus"
)

func validQuery() fxrate.Query {
	return fxrate.Query{
		From: currency.MustParse("USD"),
		To:   currency.MustParse("CNY"),
		Date: calendar.MustParseDate("2026-09-08"),
	}
}

// TestFetchReturnsOne 断言占位实现恒返回 1，与任意币种对、任意日期无关。
func TestFetchReturnsOne(t *testing.T) {
	f := Fetcher{}
	one := decimal.FromInt(1)

	queries := []fxrate.Query{
		validQuery(),
		{From: currency.MustParse("KWD"), To: currency.MustParse("CNY"), Date: calendar.MustParseDate("1970-01-01")},
		{From: currency.MustParse("JPY"), To: currency.MustParse("USD"), Date: calendar.MustParseDate("2099-12-31")},
		{From: currency.MustParse("CNY"), To: currency.MustParse("CNY"), Date: calendar.MustParseDate("2026-09-08")},
	}
	for _, q := range queries {
		t.Run(q.String(), func(t *testing.T) {
			got, err := f.Fetch(context.Background(), q)
			if err != nil {
				t.Fatalf("Fetch 失败: %v", err)
			}
			if !got.Equal(one) {
				t.Errorf("Fetch = %s, 期望 1", got)
			}
		})
	}
}

// TestRateIsOne 断言导出的 Rate 常量确为 1 且为正数。
//
// 为正数是硬要求：fxrate.Registry.Fetch 会以 IsPositive 拦下非正数，
// Rate 若被误改成 0，本实现在端口处会被判成 ErrRateNotPositive。
func TestRateIsOne(t *testing.T) {
	if !Rate.Equal(decimal.FromInt(1)) {
		t.Errorf("Rate = %s, 期望 1", Rate)
	}
	if !Rate.IsPositive() {
		t.Error("Rate 必须为正数，否则会被 fxrate.Registry.Fetch 判成 ErrRateNotPositive")
	}
}

// TestFetchViaRegistry 走通「经端口注册表调用本实现」的完整链路。
//
// 这是本实现在生产中被调用的**唯一**方式（约定 2：实现子包只被 app import），
// 故必须有一条端到端用例，而不是只测 Fetcher 本身。
func TestFetchViaRegistry(t *testing.T) {
	m := map[currency.RateSource]fxrate.Fetcher{}
	for _, c := range currency.All() {
		m[c.RateSource()] = New(context.Background())
	}
	r, err := fxrate.NewRegistry(m)
	if err != nil {
		t.Fatalf("构造注册表失败: %v", err)
	}

	got, err := r.Fetch(context.Background(), validQuery())
	if err != nil {
		t.Fatalf("经注册表调用失败: %v", err)
	}
	if !got.Equal(decimal.FromInt(1)) {
		t.Errorf("经注册表得 %s, 期望 1", got)
	}
}

// TestFetchRespectsContextCancellation 断言 ctx 取消会被响应。
//
// 终版 I-7 明写重算「可中断可续跑」。即便本实现无 IO，也必须响应取消——
// 否则一次中断要等到全部明细跑完才生效。
func TestFetchRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := Fetcher{}.Fetch(ctx, validQuery())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("ctx 已取消时应返回 context.Canceled, 实得 %v", err)
	}
	if got.Equal(decimal.FromInt(1)) {
		t.Error("取消时返回了汇率 1——即便是占位实现，失败路径也不得返回汇率")
	}
	if !got.IsZero() {
		t.Errorf("取消时应返回零值汇率, 实得 %s", got)
	}
}

// TestCancellationDistinguishableFromFetchFailure 断言经端口后仍能区分
// 「整批被中断」与「该行汇率转必填（I-5）」。
//
// service/fx 需要先判 context.Canceled 决定整批停下，而不是把一次中断误判成
// I-5 的表单故障。故 context.Canceled 与 fxrate.ErrFetch 必须同时可判定。
func TestCancellationDistinguishableFromFetchFailure(t *testing.T) {
	m := map[currency.RateSource]fxrate.Fetcher{}
	for _, c := range currency.All() {
		m[c.RateSource()] = Fetcher{}
	}
	r, err := fxrate.NewRegistry(m)
	if err != nil {
		t.Fatalf("构造注册表失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = r.Fetch(ctx, validQuery())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("context.Canceled 未穿透端口: %v", err)
	}
	if !errors.Is(err, fxrate.ErrFetch) {
		t.Errorf("应同时被包成 fxrate.ErrFetch: %v", err)
	}
}

// TestNewLogsWarnOnce 断言构造时打一条 Warn 级日志。
//
// 这条日志是本实现与 I-5「静默 1:1」之间**唯一的行为差别**（数值上二者相同），
// 因此不能省，级别也不能降到 Info（会被淹没）或升到 Error（占位是预期状态，
// 用 Error 会污染告警）。
func TestNewLogsWarnOnce(t *testing.T) {
	hook := &captureHook{}
	logrus.AddHook(hook)
	defer removeHook(hook)

	_ = New(context.Background())

	entries := hook.entries()
	if len(entries) != 1 {
		t.Fatalf("New 打了 %d 条日志, 期望恰好 1 条（每进程一次，不是每笔一次）", len(entries))
	}
	e := entries[0]
	if e.Level != logrus.WarnLevel {
		t.Errorf("日志级别为 %s, 期望 Warn——Info 会被淹没，Error 会污染告警", e.Level)
	}
	if !strings.Contains(e.Message, "占位") {
		t.Errorf("日志内容 %q 未点明这是占位实现", e.Message)
	}
	if got, ok := e.Data["rate"]; !ok || got != "1" {
		t.Errorf("日志未带上 rate=1 字段, 实得 %v", e.Data["rate"])
	}
	if _, ok := e.Data["note"]; !ok {
		t.Error("日志未带上 note 字段")
	}
}

// TestFetchLogsNothing 断言 Fetch **不打任何日志**。
//
// I-7 逐笔重算会以每笔明细一次的频率调用它；在 Fetch 里打日志就是几万行重复
// 记录，真正需要被看见的根因反而被淹没（同 fxrate 端口包注释「不打日志」）。
func TestFetchLogsNothing(t *testing.T) {
	f := New(context.Background()) // 构造日志在挂 hook 之前打完

	hook := &captureHook{}
	logrus.AddHook(hook)
	defer removeHook(hook)

	for i := 0; i < 100; i++ {
		if _, err := f.Fetch(context.Background(), validQuery()); err != nil {
			t.Fatalf("Fetch 失败: %v", err)
		}
	}
	if n := len(hook.entries()); n != 0 {
		t.Errorf("100 次 Fetch 打了 %d 条日志, 期望 0 条——I-7 逐笔调用，在此打日志会淹没根因", n)
	}
}

// TestNote 断言性质说明点明了三件事：占位、金额不正确、需 I-7 重算。
//
// 这段文本是运维在排查「为什么本位币金额等于支出金额」时的第一现场，
// 三个要点缺一不可。
func TestNote(t *testing.T) {
	for _, want := range []string{"占位", "不正确", "I-7"} {
		if !strings.Contains(Note, want) {
			t.Errorf("Note 未包含 %q: %s", want, Note)
		}
	}
	if got := (Fetcher{}).Note(); got != Note {
		t.Errorf("Fetcher.Note() 与包级 Note 不一致")
	}
}

// TestFetcherIsStateless 断言 Fetcher 无字段，因而不可变、可并发。
//
// 一旦有人给它加上字段（缓存、计数器、HTTP 客户端），「零值可用 + 可并发」
// 两条性质就同时失效，而本包的注释是按无状态写的。
func TestFetcherIsStateless(t *testing.T) {
	if size := unsafe.Sizeof(Fetcher{}); size != 0 {
		t.Errorf("Fetcher 大小为 %d 字节, 期望 0——本实现应无状态；"+
			"新增字段会让「零值可用」与「可并发」两条性质同时失效", size)
	}
}

// TestFetchConcurrent 在 -race 下并发调用。
func TestFetchConcurrent(t *testing.T) {
	f := Fetcher{}
	var wg sync.WaitGroup
	const n = 32
	wg.Add(n)
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				got, err := f.Fetch(context.Background(), validQuery())
				if err != nil {
					errCh <- err
					return
				}
				if !got.Equal(decimal.FromInt(1)) {
					errCh <- errors.New("并发下取到 " + got.String())
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("并发 Fetch 失败: %v", err)
	}
}

// --- 测试辅助 ---

// captureHook 捕获 logrus 日志条目。
type captureHook struct {
	mu   sync.Mutex
	logs []logrus.Entry
}

func (h *captureHook) Levels() []logrus.Level { return logrus.AllLevels }

func (h *captureHook) Fire(e *logrus.Entry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs = append(h.logs, *e)
	return nil
}

func (h *captureHook) entries() []logrus.Entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]logrus.Entry, len(h.logs))
	copy(out, h.logs)
	return out
}

// removeHook 摘掉 hook。logrus 无 RemoveHook，故重建 hook 集合。
//
// 只在测试内使用；用例之间因此互不串扰。
func removeHook(target logrus.Hook) {
	std := logrus.StandardLogger()
	std.ReplaceHooks(logrus.LevelHooks{})
	_ = target
}
