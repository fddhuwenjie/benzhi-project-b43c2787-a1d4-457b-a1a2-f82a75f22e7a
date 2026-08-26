package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"time"

	"astroplate-vault/internal/domain"
	_ "modernc.org/sqlite"
)

func scanBatch(scanner interface{ Scan(...any) error }) (*domain.PlateBatch, error) {
	var raw []byte
	if err := scanner.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.NewError(domain.CodeNotFound, "批次不存在")
		}
		return nil, err
	}
	return decodeBatchSnapshot(raw)
}

func decodeBatchSnapshot(raw []byte) (*domain.PlateBatch, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("批次快照为空")
	}
	var batch domain.PlateBatch
	if err := json.Unmarshal(raw, &batch); err != nil {
		return nil, fmt.Errorf("解码批次快照失败: %v", err)
	}
	if len(batch.Calibrations) == 0 && batch.Calibration != nil {
		batch.Calibrations = []domain.CalibrationSession{*batch.Calibration}
	}
	if len(batch.Calibrations) > 0 {
		batch.Calibration = &batch.Calibrations[len(batch.Calibrations)-1]
	}
	for i := range batch.PeerReviews {
		if batch.PeerReviews[i].Evidence == nil {
			batch.PeerReviews[i].Evidence = []domain.PeerReviewEvidence{}
		}
	}
	if err := domain.ValidateAggregate(&batch); err != nil {
		return nil, fmt.Errorf("校验批次快照失败: %v", err)
	}
	return &batch, nil
}

func (s *Store) LoadBatch(ctx context.Context, id string) (*domain.PlateBatch, error) {
	return scanBatch(s.db.QueryRowContext(ctx, `SELECT snapshot FROM plate_batches WHERE id=?`, id))
}

type BatchListFilter struct {
	State, ScannerID, QualityPolicyVersion, CreatedBy, Title string
}

type BatchListRecord struct {
	Batch     *domain.PlateBatch
	UpdatedAt time.Time
}

// ListBatchRecords 在关系库中执行组合筛选；应用层随后逐项核对快照与关系投影。
func (s *Store) ListBatchRecords(ctx context.Context, filter BatchListFilter) ([]BatchListRecord, error) {
	query := `SELECT snapshot,updated_at FROM plate_batches WHERE 1=1`
	args := []any{}
	if filter.State != "" {
		query += ` AND state=?`
		args = append(args, filter.State)
	}
	if filter.ScannerID != "" {
		query += ` AND json_extract(snapshot,'$.scanner_id')=?`
		args = append(args, filter.ScannerID)
	}
	if filter.QualityPolicyVersion != "" {
		query += ` AND json_extract(snapshot,'$.quality_policy_version')=?`
		args = append(args, filter.QualityPolicyVersion)
	}
	if filter.CreatedBy != "" {
		query += ` AND json_extract(snapshot,'$.created_by')=?`
		args = append(args, filter.CreatedBy)
	}
	if filter.Title != "" {
		query += ` AND instr(lower(json_extract(snapshot,'$.title')),lower(?))>0`
		args = append(args, filter.Title)
	}
	query += ` ORDER BY updated_at DESC,id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BatchListRecord{}
	for rows.Next() {
		var raw []byte
		var updated string
		if err = rows.Scan(&raw, &updated); err != nil {
			return nil, err
		}
		var batch domain.PlateBatch
		if err = json.Unmarshal(raw, &batch); err != nil {
			return nil, domain.NewError(domain.CodeIntegrity, "批次列表包含无法解码的聚合快照")
		}
		if err = domain.ValidateAggregate(&batch); err != nil {
			return nil, err
		}
		at, parseErr := time.Parse(time.RFC3339Nano, updated)
		if parseErr != nil {
			return nil, domain.NewError(domain.CodeIntegrity, "批次 %s 的 updated_at 无效", batch.ID)
		}
		out = append(out, BatchListRecord{Batch: &batch, UpdatedAt: at})
	}
	return out, rows.Err()
}
