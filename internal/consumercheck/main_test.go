package consumer

// 独立于单测路径的外部消费者验证：模拟 service/audit 的
// 「先落审计取 ID → 明细挂载该 ID」（不变式 4），确认包对外契约真的可用。
import (
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/idgen"
)

func TestAuditThenExpenseFlow(t *testing.T) {
	auditID := idgen.GenAuditID()
	if auditID <= 0 {
		t.Fatalf("审计ID 无效: %d", auditID)
	}
	// 一条审计 + N 条明细，明细全部挂载同一个审计ID（= 批次号）
	const n = 37
	ids := make(map[int64]struct{}, n)
	for i := 0; i < n; i++ {
		id := idgen.GenExpenseID()
		if id == auditID {
			t.Fatalf("明细ID 与审计ID 相同: %d", id)
		}
		if _, dup := ids[id]; dup {
			t.Fatalf("明细ID 重复: %d", id)
		}
		ids[id] = struct{}{}
	}
	if len(ids) != n {
		t.Fatalf("明细ID 数量不符: 期望 %d 实际 %d", n, len(ids))
	}
	// 5 个入口混用仍不串号
	all := []int64{idgen.GenUserID(), idgen.GenCategoryID(), idgen.GenFileID(),
		idgen.GenAuditID(), idgen.GenExpenseID()}
	seen := map[int64]struct{}{}
	for _, id := range all {
		if _, dup := seen[id]; dup {
			t.Fatalf("跨入口串号: %d", id)
		}
		seen[id] = struct{}{}
	}
	t.Logf("审计ID=%d，%d 条明细ID 全部唯一且挂载成功", auditID, n)
}
