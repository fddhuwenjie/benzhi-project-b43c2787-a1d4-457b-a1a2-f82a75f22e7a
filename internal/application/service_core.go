package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"sync"
	"time"

	"astroplate-vault/internal/audit"
	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

type Clock func() time.Time

type Service struct {
	store              *persistence.Store
	now                Clock
	locks              sync.Map
	auditVerifications sync.Map
}

func NewService(store *persistence.Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) lock(batchID string) func() {
	value, _ := s.locks.LoadOrStore(batchID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func newID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%x-%x-%x-%x-%x", prefix, b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func validateMeta(meta CommandMeta, create bool) error {
	if err := domain.ValidateIdentifier("request_id", meta.RequestID); err != nil {
		return err
	}
	if err := domain.ValidatePrincipal("actor", meta.Actor); err != nil {
		return err
	}
	if create && meta.ExpectedRevision != 0 {
		return domain.NewError(domain.CodeValidation, "创建命令的 expected_revision 必须为 0")
	}
	if !create && meta.ExpectedRevision < 1 {
		return domain.NewError(domain.CodeValidation, "expected_revision 必须为正整数")
	}
	return nil
}

func payloadHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func replayOrConflict(record *persistence.IdempotencyRecord, operation, hash string) (CommandResult, error) {
	if record.Operation != operation || record.PayloadHash != hash {
		return CommandResult{}, domain.NewError(domain.CodeIdempotency, "request_id 已用于不同命令或载荷")
	}
	var result CommandResult
	if err := json.Unmarshal(record.Response, &result); err != nil {
		return result, err
	}
	result.Replayed = true
	return result, nil
}

func putResult(ctx context.Context, tx *persistence.Tx, requestID, batchID, operation, hash string, result CommandResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return tx.PutIdempotency(ctx, persistence.IdempotencyRecord{RequestID: requestID, BatchID: batchID, Operation: operation, PayloadHash: hash, Response: raw, Status: 200})
}

func appendEvent(ctx context.Context, tx *persistence.Tx, batch *domain.PlateBatch, eventType, actor string, payload any, now time.Time) error {
	last, err := tx.LastAudit(ctx, batch.ID)
	if err != nil {
		return err
	}
	event, err := audit.NewEvent(batch.ID, last.Sequence+1, eventType, batch.Revision, actor, payload, now, last.EventHash)
	if err != nil {
		return err
	}
	return tx.AppendAudit(ctx, event)
}
