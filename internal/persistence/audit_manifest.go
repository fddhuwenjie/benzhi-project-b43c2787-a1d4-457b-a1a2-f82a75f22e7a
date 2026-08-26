package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"time"

	"astroplate-vault/internal/domain"
	_ "modernc.org/sqlite"
)

func (t *Tx) LastAudit(ctx context.Context, batchID string) (domain.AuditEvent, error) {
	var e domain.AuditEvent
	var at string
	err := t.tx.QueryRowContext(ctx, `SELECT batch_id,sequence,event_type,revision,actor,payload,occurred_at,previous_hash,event_hash FROM audit_events WHERE batch_id=? ORDER BY sequence DESC LIMIT 1`, batchID).Scan(&e.BatchID, &e.Sequence, &e.EventType, &e.Revision, &e.Actor, &e.Payload, &at, &e.PreviousHash, &e.EventHash)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AuditEvent{}, nil
	}
	if err != nil {
		return e, err
	}
	e.OccurredAt, err = time.Parse(time.RFC3339Nano, at)
	return e, err
}

func (t *Tx) AppendAudit(ctx context.Context, e domain.AuditEvent) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO audit_events(batch_id,sequence,event_type,revision,actor,payload,occurred_at,previous_hash,event_hash) VALUES(?,?,?,?,?,?,?,?,?)`, e.BatchID, e.Sequence, e.EventType, e.Revision, e.Actor, []byte(e.Payload), e.OccurredAt.Format(time.RFC3339Nano), e.PreviousHash, e.EventHash)
	return err
}

func scanEvents(rows *sql.Rows) ([]domain.AuditEvent, error) {
	defer rows.Close()
	events := []domain.AuditEvent{}
	for rows.Next() {
		var e domain.AuditEvent
		var at string
		if err := rows.Scan(&e.BatchID, &e.Sequence, &e.EventType, &e.Revision, &e.Actor, &e.Payload, &at, &e.PreviousHash, &e.EventHash); err != nil {
			return nil, err
		}
		var err error
		e.OccurredAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) ListAudit(ctx context.Context, batchID string, after int64, limit int) ([]domain.AuditEvent, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT batch_id,sequence,event_type,revision,actor,payload,occurred_at,previous_hash,event_hash FROM audit_events WHERE batch_id=? AND sequence>? ORDER BY sequence LIMIT ?`, batchID, after, limit)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (s *Store) ListAuditFiltered(ctx context.Context, batchID, eventType string, after, before int64, limit int) ([]domain.AuditEvent, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	q := `SELECT batch_id,sequence,event_type,revision,actor,payload,occurred_at,previous_hash,event_hash FROM audit_events WHERE batch_id=? AND sequence>?`
	args := []any{batchID, after}
	if before > 0 {
		q += " AND sequence<?"
		args = append(args, before)
	}
	if eventType != "" {
		q += " AND event_type=?"
		args = append(args, eventType)
	}
	q += " ORDER BY sequence LIMIT ?"
	args = append(args, limit)
	resource, err := s.auditFilterStatement(ctx, q)
	if err != nil {
		return nil, err
	}
	defer resource.statement.Close()
	defer resource.connection.Close()
	rows, err := resource.statement.QueryContext(ctx, args...)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (s *Store) auditFilterStatement(ctx context.Context, query string) (*cachedAuditStatement, error) {
	s.auditStatementMu.Lock()
	defer s.auditStatementMu.Unlock()
	if resource := s.auditStatementCache[query]; resource != nil {
		return resource, nil
	}
	connection, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	statement, err := connection.PrepareContext(ctx, query)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	resource := &cachedAuditStatement{connection: connection, statement: statement}
	s.auditStatementCache[query] = resource
	return resource, nil
}

func (s *Store) AllAudit(ctx context.Context, batchID string) ([]domain.AuditEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT batch_id,sequence,event_type,revision,actor,payload,occurred_at,previous_hash,event_hash FROM audit_events WHERE batch_id=? ORDER BY sequence`, batchID)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (t *Tx) AllAudit(ctx context.Context, batchID string) ([]domain.AuditEvent, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT batch_id,sequence,event_type,revision,actor,payload,occurred_at,previous_hash,event_hash FROM audit_events WHERE batch_id=? ORDER BY sequence`, batchID)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (s *Store) BatchIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM plate_batches ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (t *Tx) PutManifest(ctx context.Context, m domain.ArchiveManifest) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO archive_manifests(batch_id,manifest_hash,document,created_at) VALUES(?,?,?,?)`, m.BatchID, m.ManifestHash, raw, m.SealedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) LoadManifest(ctx context.Context, batchID string) (domain.ArchiveManifest, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT document FROM archive_manifests WHERE batch_id=?`, batchID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ArchiveManifest{}, domain.NewError(domain.CodeNotFound, "封存清单不存在")
	}
	if err != nil {
		return domain.ArchiveManifest{}, err
	}
	var m domain.ArchiveManifest
	err = json.Unmarshal(raw, &m)
	return m, err
}
