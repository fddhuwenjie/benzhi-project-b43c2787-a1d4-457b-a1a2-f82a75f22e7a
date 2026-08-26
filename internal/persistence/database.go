package persistence

import (
	"context"
	"database/sql"

	"fmt"

	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }
type Tx struct{ tx *sql.Tx }

type IdempotencyRecord struct {
	RequestID   string
	BatchID     string
	Operation   string
	PayloadHash string
	Response    []byte
	Status      int
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("数据库路径不能为空")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if err = store.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS plate_batches (id TEXT PRIMARY KEY, revision INTEGER NOT NULL, state TEXT NOT NULL, snapshot BLOB NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS idempotency_records (request_id TEXT PRIMARY KEY, batch_id TEXT NOT NULL, operation TEXT NOT NULL, payload_hash TEXT NOT NULL, response BLOB NOT NULL, status INTEGER NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_idempotency_batch ON idempotency_records(batch_id)`,
		`CREATE TABLE IF NOT EXISTS audit_events (batch_id TEXT NOT NULL, sequence INTEGER NOT NULL, event_type TEXT NOT NULL, revision INTEGER NOT NULL, actor TEXT NOT NULL, payload BLOB NOT NULL, occurred_at TEXT NOT NULL, previous_hash TEXT NOT NULL, event_hash TEXT NOT NULL, PRIMARY KEY(batch_id, sequence), UNIQUE(batch_id, event_hash))`,
		`CREATE TABLE IF NOT EXISTS archive_manifests (batch_id TEXT PRIMARY KEY, manifest_hash TEXT NOT NULL, document BLOB NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS calibration_sessions (id TEXT PRIMARY KEY, batch_id TEXT NOT NULL, resolution_dpi INTEGER NOT NULL, gray_response_error REAL NOT NULL, geometry_error_percent REAL NOT NULL, performed_by TEXT NOT NULL, performed_at TEXT NOT NULL, result TEXT NOT NULL, FOREIGN KEY(batch_id) REFERENCES plate_batches(id))`,
		`DROP INDEX IF EXISTS idx_calibration_batch`,
		`CREATE INDEX IF NOT EXISTS idx_calibration_batch_time ON calibration_sessions(batch_id,performed_at,id)`,
		`CREATE TABLE IF NOT EXISTS plate_scans (id TEXT PRIMARY KEY, batch_id TEXT NOT NULL, catalog_number INTEGER NOT NULL, version INTEGER NOT NULL, content_checksum TEXT NOT NULL, pixel_width INTEGER NOT NULL, pixel_height INTEGER NOT NULL, bit_depth INTEGER NOT NULL, exposure_score REAL NOT NULL, focus_score REAL NOT NULL, supersedes_scan_id TEXT NOT NULL, captured_by TEXT NOT NULL, captured_at TEXT NOT NULL, UNIQUE(batch_id,content_checksum), FOREIGN KEY(batch_id) REFERENCES plate_batches(id))`,
		`CREATE INDEX IF NOT EXISTS idx_scans_catalog ON plate_scans(batch_id,catalog_number,version)`,
		`CREATE TABLE IF NOT EXISTS quality_issues (id TEXT PRIMARY KEY, batch_id TEXT NOT NULL, scan_id TEXT NOT NULL, rule_code TEXT NOT NULL, severity TEXT NOT NULL, observed_value REAL NOT NULL, threshold_text TEXT NOT NULL, resolution_kind TEXT NOT NULL, resolution_note TEXT NOT NULL, replacement_scan_id TEXT NOT NULL, status TEXT NOT NULL, resolved_by TEXT NOT NULL, resolved_at TEXT NOT NULL, resolution_history BLOB NOT NULL DEFAULT '[]', FOREIGN KEY(batch_id) REFERENCES plate_batches(id))`,
		`CREATE INDEX IF NOT EXISTS idx_issues_batch_status ON quality_issues(batch_id,status)`,
		`CREATE TABLE IF NOT EXISTS quality_conclusions (batch_id TEXT NOT NULL, scan_id TEXT NOT NULL, catalog_number INTEGER NOT NULL, passed INTEGER NOT NULL, document BLOB NOT NULL, PRIMARY KEY(batch_id,scan_id), FOREIGN KEY(batch_id) REFERENCES plate_batches(id))`,
		`CREATE TABLE IF NOT EXISTS peer_reviews (batch_id TEXT NOT NULL, ordinal INTEGER NOT NULL, reviewer TEXT NOT NULL, sample_catalogs BLOB NOT NULL, passed INTEGER NOT NULL, note TEXT NOT NULL, reviewed_at TEXT NOT NULL, evidence BLOB NOT NULL DEFAULT '[]', PRIMARY KEY(batch_id,ordinal), FOREIGN KEY(batch_id) REFERENCES plate_batches(id))`,
		`CREATE TABLE IF NOT EXISTS peer_review_drafts (id TEXT PRIMARY KEY, batch_id TEXT NOT NULL, round_number INTEGER NOT NULL, reviewer TEXT NOT NULL, base_batch_revision INTEGER NOT NULL, draft_revision INTEGER NOT NULL, status TEXT NOT NULL, document BLOB NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, completed_at TEXT NOT NULL, FOREIGN KEY(batch_id) REFERENCES plate_batches(id))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_peer_draft_open_reviewer ON peer_review_drafts(batch_id,round_number,reviewer) WHERE status='open'`,
		`CREATE INDEX IF NOT EXISTS idx_peer_draft_batch ON peer_review_drafts(batch_id,round_number,status)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_batch_actor_revision ON audit_events(batch_id,actor,revision,event_type)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("执行数据库迁移: %w", err)
		}
	}

	if _, err := s.db.ExecContext(ctx, `ALTER TABLE peer_reviews ADD COLUMN evidence BLOB NOT NULL DEFAULT '[]'`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("迁移同行抽验证据: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE quality_issues ADD COLUMN resolution_history BLOB NOT NULL DEFAULT '[]'`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("迁移质量裁决历史: %w", err)
	}
	if err := s.migratePlateScanLineage(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) migratePlateScanLineage(ctx context.Context) error {
	var schema string
	if err := s.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='plate_scans'`).Scan(&schema); err != nil {
		return err
	}
	normalized := strings.ToLower(strings.ReplaceAll(schema, " ", ""))
	if !strings.Contains(normalized, "unique(batch_id,catalog_number,version)") {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE plate_scans_lineage (id TEXT PRIMARY KEY, batch_id TEXT NOT NULL, catalog_number INTEGER NOT NULL, version INTEGER NOT NULL, content_checksum TEXT NOT NULL, pixel_width INTEGER NOT NULL, pixel_height INTEGER NOT NULL, bit_depth INTEGER NOT NULL, exposure_score REAL NOT NULL, focus_score REAL NOT NULL, supersedes_scan_id TEXT NOT NULL, captured_by TEXT NOT NULL, captured_at TEXT NOT NULL, UNIQUE(batch_id,content_checksum), FOREIGN KEY(batch_id) REFERENCES plate_batches(id))`,
		`INSERT INTO plate_scans_lineage SELECT id,batch_id,catalog_number,version,content_checksum,pixel_width,pixel_height,bit_depth,exposure_score,focus_score,supersedes_scan_id,captured_by,captured_at FROM plate_scans`,
		`DROP TABLE plate_scans`,
		`ALTER TABLE plate_scans_lineage RENAME TO plate_scans`,
		`CREATE INDEX IF NOT EXISTS idx_scans_catalog ON plate_scans(batch_id,catalog_number,version)`,
	}
	for _, statement := range statements {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("迁移扫描替代谱系: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("提交扫描替代谱系迁移: %w", err)
	}
	return nil
}

func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	w := &Tx{tx: tx}
	if err = fn(w); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("提交事务: %w", err)
	}
	return nil
}

