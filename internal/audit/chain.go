package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"astroplate-vault/internal/domain"
)

type hashMaterial struct {
	BatchID      string          `json:"batch_id"`
	Sequence     int64           `json:"sequence"`
	EventType    string          `json:"event_type"`
	Revision     int64           `json:"revision"`
	Actor        string          `json:"actor"`
	Payload      json.RawMessage `json:"payload"`
	OccurredAt   string          `json:"occurred_at"`
	PreviousHash string          `json:"previous_hash"`
}

func CanonicalPayload(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("编码审计载荷: %w", err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, fmt.Errorf("规范化审计载荷: %w", err)
	}
	return compact.Bytes(), nil
}

func NewEvent(batchID string, sequence int64, eventType string, revision int64, actor string, payload any, at time.Time, previous string) (domain.AuditEvent, error) {
	if batchID == "" || sequence < 1 || eventType == "" || actor == "" {
		return domain.AuditEvent{}, domain.NewError(domain.CodeValidation, "审计事件字段不完整")
	}
	raw, err := CanonicalPayload(payload)
	if err != nil {
		return domain.AuditEvent{}, err
	}
	e := domain.AuditEvent{BatchID: batchID, Sequence: sequence, EventType: eventType, Revision: revision, Actor: actor, Payload: raw, OccurredAt: at.UTC(), PreviousHash: previous}
	e.EventHash, err = CalculateHash(e)
	return e, err
}

func CalculateHash(e domain.AuditEvent) (string, error) {
	m := hashMaterial{BatchID: e.BatchID, Sequence: e.Sequence, EventType: e.EventType, Revision: e.Revision, Actor: e.Actor, Payload: e.Payload, OccurredAt: e.OccurredAt.UTC().Format(time.RFC3339Nano), PreviousHash: e.PreviousHash}
	raw, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func Verify(events []domain.AuditEvent) error {
	previous := ""
	for i, event := range events {
		expectedSequence := int64(i + 1)
		if event.Sequence != expectedSequence {
			return domain.NewError(domain.CodeIntegrity, "审计事件缺号：期望 %d，得到 %d", expectedSequence, event.Sequence)
		}
		if event.PreviousHash != previous {
			return domain.NewError(domain.CodeIntegrity, "审计事件 %d 的前序摘要不匹配", event.Sequence)
		}
		hash, err := CalculateHash(event)
		if err != nil {
			return err
		}
		if hash != event.EventHash {
			return domain.NewError(domain.CodeIntegrity, "审计事件 %d 内容摘要校验失败", event.Sequence)
		}
		previous = event.EventHash
	}
	return nil
}

func Head(events []domain.AuditEvent) string {
	if len(events) == 0 {
		return ""
	}
	return events[len(events)-1].EventHash
}
