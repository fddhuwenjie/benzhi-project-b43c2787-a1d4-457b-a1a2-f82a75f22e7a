package application

import (
	"context"

	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

func (s *Service) CreateBatch(ctx context.Context, request CreateBatchRequest) (CommandResult, error) {
	if err := validateMeta(request.CommandMeta, true); err != nil {
		return CommandResult{}, err
	}
	hash, err := payloadHash(request)
	if err != nil {
		return CommandResult{}, err
	}
	unlock := s.lock("create:" + request.RequestID)
	defer unlock()
	var result CommandResult
	err = s.store.WithTx(ctx, func(tx *persistence.Tx) error {
		record, err := tx.GetIdempotency(ctx, request.RequestID)
		if err != nil {
			return err
		}
		if record != nil {
			result, err = replayOrConflict(record, request.ID, "create_batch", hash)
			return err
		}
		id := request.ID
		if id == "" {
			id = newID("batch")
		}
		batch, err := domain.NewBatch(id, request.Title, request.CatalogStart, request.CatalogEnd, request.ScannerID, request.QualityPolicyVersion, request.Actor, s.now())
		if err != nil {
			return err
		}
		if err = tx.InsertBatch(ctx, batch); err != nil {
			return err
		}
		if err = appendEvent(ctx, tx, batch, "batch.created", request.Actor, map[string]any{"request_id": request.RequestID, "catalog_start": batch.CatalogStart, "catalog_end": batch.CatalogEnd, "scanner_id": batch.ScannerID, "quality_policy_version": batch.QualityPolicyVersion}, s.now()); err != nil {
			return err
		}
		result = CommandResult{Batch: batch}
		return putResult(ctx, tx, request.RequestID, batch.ID, "create_batch", hash, result)
	})
	return result, err
}

