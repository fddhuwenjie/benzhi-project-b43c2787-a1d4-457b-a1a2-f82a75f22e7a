package application

import (
	"context"

	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

type mutateFunc func(*domain.PlateBatch) (any, error)

func (s *Service) mutate(ctx context.Context, batchID, operation, eventType string, meta CommandMeta, request any, fn mutateFunc) (CommandResult, error) {
	if err := validateMeta(meta, false); err != nil {
		return CommandResult{}, err
	}
	if batchID == "" {
		return CommandResult{}, domain.NewError(domain.CodeValidation, "batch_id 不能为空")
	}
	hash, err := payloadHash(request)
	if err != nil {
		return CommandResult{}, err
	}
	unlock := s.lock(batchID)
	defer unlock()
	var result CommandResult
	err = s.store.WithTx(ctx, func(tx *persistence.Tx) error {
		record, err := tx.GetIdempotency(ctx, meta.RequestID)
		if err != nil {
			return err
		}
		if record != nil {
			result, err = replayOrConflict(record, operation, hash)
			return err
		}
		batch, err := tx.LoadBatch(ctx, batchID)
		if err != nil {
			return err
		}
		if batch.Revision != meta.ExpectedRevision {
			return domain.RevisionConflict(batch.Revision)
		}
		stored := batch.Revision
		detail, err := fn(batch)
		if err != nil {
			return err
		}
		if err = tx.SaveBatch(ctx, batch, stored); err != nil {
			return err
		}
		if err = appendEvent(ctx, tx, batch, eventType, meta.Actor, map[string]any{"request_id": meta.RequestID, "detail": detail}, s.now()); err != nil {
			return err
		}
		result = CommandResult{Batch: batch}
		if summary, ok := detail.(domain.QualitySummary); ok {
			result.QualitySummary = &summary
		}
		return putResult(ctx, tx, meta.RequestID, batchID, operation, hash, result)
	})
	return result, err
}

