package audit

import (
	"testing"
	"time"

	"astroplate-vault/internal/domain"
)

func TestAuditChainDetectsTamperingAndGaps(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	first, err := NewEvent("batch-1", 1, "batch.created", 1, "operator", map[string]any{"range": "1-2"}, now, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewEvent("batch-1", 2, "scan.registered", 2, "operator", map[string]any{"catalog": 1}, now.Add(time.Second), first.EventHash)
	if err != nil {
		t.Fatal(err)
	}
	if err = Verify([]domain.AuditEvent{first, second}); err != nil {
		t.Fatalf("有效链校验失败: %v", err)
	}
	tampered := second
	tampered.Actor = "intruder"
	if err = Verify([]domain.AuditEvent{first, tampered}); err == nil {
		t.Fatal("篡改事件未被识别")
	}
	gap := second
	gap.Sequence = 3
	if err = Verify([]domain.AuditEvent{first, gap}); err == nil {
		t.Fatal("事件缺号未被识别")
	}
}
