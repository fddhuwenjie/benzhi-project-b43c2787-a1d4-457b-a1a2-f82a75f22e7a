package persistence

import (
	"context"

	"encoding/json"

	"fmt"

	"time"

	"astroplate-vault/internal/domain"
	_ "modernc.org/sqlite"
)

func (t *Tx) LoadBatch(ctx context.Context, id string) (*domain.PlateBatch, error) {
	return scanBatch(t.tx.QueryRowContext(ctx, `SELECT snapshot FROM plate_batches WHERE id=?`, id))
}

func encodeBatch(batch *domain.PlateBatch) ([]byte, error) {
	raw, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("编码批次快照: %w", err)
	}
	return raw, nil
}

func (t *Tx) InsertBatch(ctx context.Context, batch *domain.PlateBatch) error {
	if err := domain.ValidateAggregate(batch); err != nil {
		return err
	}
	raw, err := encodeBatch(batch)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO plate_batches(id,revision,state,snapshot,updated_at) VALUES(?,?,?,?,?)`, batch.ID, batch.Revision, batch.State, raw, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return domain.NewError(domain.CodeDuplicate, "批次 ID 已存在")
	}
	return t.replaceProjection(ctx, batch)
}

func (t *Tx) SaveBatch(ctx context.Context, batch *domain.PlateBatch, expectedStoredRevision int64) error {
	if err := domain.ValidateAggregate(batch); err != nil {
		return err
	}
	raw, err := encodeBatch(batch)
	if err != nil {
		return err
	}
	result, err := t.tx.ExecContext(ctx, `UPDATE plate_batches SET revision=?,state=?,snapshot=?,updated_at=? WHERE id=? AND revision=?`, batch.Revision, batch.State, raw, time.Now().UTC().Format(time.RFC3339Nano), batch.ID, expectedStoredRevision)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return domain.RevisionConflict(expectedStoredRevision)
	}
	return t.replaceProjection(ctx, batch)
}

