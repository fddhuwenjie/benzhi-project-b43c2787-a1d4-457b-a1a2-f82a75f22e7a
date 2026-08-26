package persistence

import (
	"context"
	"database/sql"

	"errors"

	"time"

	_ "modernc.org/sqlite"
)

func (t *Tx) GetIdempotency(ctx context.Context, requestID string) (*IdempotencyRecord, error) {
	r := &IdempotencyRecord{}
	err := t.tx.QueryRowContext(ctx, `SELECT request_id,batch_id,operation,payload_hash,response,status FROM idempotency_records WHERE request_id=?`, requestID).Scan(&r.RequestID, &r.BatchID, &r.Operation, &r.PayloadHash, &r.Response, &r.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func (s *Store) GetIdempotency(ctx context.Context, requestID string) (*IdempotencyRecord, error) {
	r := &IdempotencyRecord{}
	err := s.db.QueryRowContext(ctx, `SELECT request_id,batch_id,operation,payload_hash,response,status FROM idempotency_records WHERE request_id=?`, requestID).Scan(&r.RequestID, &r.BatchID, &r.Operation, &r.PayloadHash, &r.Response, &r.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func (t *Tx) PutIdempotency(ctx context.Context, r IdempotencyRecord) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO idempotency_records(request_id,batch_id,operation,payload_hash,response,status,created_at) VALUES(?,?,?,?,?,?,?)`, r.RequestID, r.BatchID, r.Operation, r.PayloadHash, r.Response, r.Status, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

