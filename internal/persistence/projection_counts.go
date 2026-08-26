package persistence

import (
	"bytes"
	"context"

	"encoding/json"

	"sort"

	"time"

	"astroplate-vault/internal/domain"
	_ "modernc.org/sqlite"
)

type ProjectionCounts struct {
	Calibrations int `json:"calibrations"`
	Scans        int `json:"scans"`
	Issues       int `json:"issues"`
	Conclusions  int `json:"conclusions"`
	PeerReviews  int `json:"peer_reviews"`
}

func (s *Store) ProjectionCounts(ctx context.Context, batchID string) (ProjectionCounts, error) {
	var counts ProjectionCounts
	queries := []struct {
		sql    string
		target *int
	}{{`SELECT COUNT(*) FROM calibration_sessions WHERE batch_id=?`, &counts.Calibrations}, {`SELECT COUNT(*) FROM plate_scans WHERE batch_id=?`, &counts.Scans}, {`SELECT COUNT(*) FROM quality_issues WHERE batch_id=?`, &counts.Issues}, {`SELECT COUNT(*) FROM quality_conclusions WHERE batch_id=?`, &counts.Conclusions}, {`SELECT COUNT(*) FROM peer_reviews WHERE batch_id=?`, &counts.PeerReviews}}
	for _, query := range queries {
		if err := s.db.QueryRowContext(ctx, query.sql, batchID).Scan(query.target); err != nil {
			return counts, err
		}
	}
	return counts, nil
}

func (t *Tx) ValidateProjectionCounts(ctx context.Context, batch *domain.PlateBatch) error {
	wants := []struct {
		table string
		count int
	}{
		{"calibration_sessions", len(batch.Calibrations)}, {"plate_scans", len(batch.Scans)}, {"quality_issues", len(batch.Issues)}, {"quality_conclusions", len(batch.QualityConclusions)}, {"peer_reviews", len(batch.PeerReviews)},
	}
	for _, want := range wants {
		var actual int
		if err := t.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+want.table+` WHERE batch_id=?`, batch.ID).Scan(&actual); err != nil {
			return err
		}
		if actual != want.count {
			return domain.NewError(domain.CodeIntegrity, "批次 %s 的%s关系投影数量不一致", batch.ID, want.table)
		}
	}
	return nil
}

func (s *Store) ValidateBatchProjection(ctx context.Context, batch *domain.PlateBatch) error {
	counts, err := s.ProjectionCounts(ctx, batch.ID)
	if err != nil {
		return err
	}
	if counts.Calibrations != len(batch.Calibrations) || counts.Scans != len(batch.Scans) || counts.Issues != len(batch.Issues) || counts.Conclusions != len(batch.QualityConclusions) || counts.PeerReviews != len(batch.PeerReviews) {
		return domain.NewError(domain.CodeIntegrity, "批次 %s 的关系投影与聚合快照数量不一致", batch.ID)
	}
	if err = s.validateCalibrationProjection(ctx, batch); err != nil {
		return err
	}
	if err = s.validateScanProjection(ctx, batch); err != nil {
		return err
	}
	if err = s.validateIssueProjection(ctx, batch); err != nil {
		return err
	}
	if err = s.validateConclusionProjection(ctx, batch); err != nil {
		return err
	}
	return s.validatePeerReviewProjection(ctx, batch)
}

func projectionMismatch(batchID, entity string) error {
	return domain.NewError(domain.CodeIntegrity, "批次 %s 的%s投影与聚合快照不一致", batchID, entity)
}

func (s *Store) validateCalibrationProjection(ctx context.Context, batch *domain.PlateBatch) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,resolution_dpi,gray_response_error,geometry_error_percent,performed_by,performed_at,result FROM calibration_sessions WHERE batch_id=? ORDER BY performed_at,id`, batch.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	expected := append([]domain.CalibrationSession(nil), batch.Calibrations...)
	sort.Slice(expected, func(i, j int) bool {
		if expected[i].PerformedAt.Equal(expected[j].PerformedAt) {
			return expected[i].ID < expected[j].ID
		}
		return expected[i].PerformedAt.Before(expected[j].PerformedAt)
	})
	i := 0
	for rows.Next() {
		if i >= len(expected) {
			return projectionMismatch(batch.ID, "标定")
		}
		var id, result, performedBy, performedAt string
		var dpi int
		var gray, geometry float64
		if err = rows.Scan(&id, &dpi, &gray, &geometry, &performedBy, &performedAt, &result); err != nil {
			return err
		}
		c := expected[i]
		if id != c.ID || dpi != c.ResolutionDPI || gray != c.GrayResponseError || geometry != c.GeometryErrorPercent || performedBy != c.PerformedBy || performedAt != c.PerformedAt.Format(time.RFC3339Nano) || result != c.Result {
			return projectionMismatch(batch.ID, "标定")
		}
		i++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if i != len(expected) {
		return projectionMismatch(batch.ID, "标定")
	}
	return nil
}

func (s *Store) validateScanProjection(ctx context.Context, batch *domain.PlateBatch) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,catalog_number,version,content_checksum,pixel_width,pixel_height,bit_depth,exposure_score,focus_score,supersedes_scan_id,captured_by,captured_at FROM plate_scans WHERE batch_id=? ORDER BY catalog_number,version,id`, batch.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	expected := append([]domain.PlateScan(nil), batch.Scans...)
	sort.Slice(expected, func(i, j int) bool {
		if expected[i].CatalogNumber == expected[j].CatalogNumber {
			if expected[i].Version == expected[j].Version {
				return expected[i].ID < expected[j].ID
			}
			return expected[i].Version < expected[j].Version
		}
		return expected[i].CatalogNumber < expected[j].CatalogNumber
	})
	index := 0
	for rows.Next() {
		if index >= len(expected) {
			return projectionMismatch(batch.ID, "扫描")
		}
		var actual domain.PlateScan
		var at string
		if err = rows.Scan(&actual.ID, &actual.CatalogNumber, &actual.Version, &actual.ContentChecksum, &actual.PixelWidth, &actual.PixelHeight, &actual.BitDepth, &actual.ExposureScore, &actual.FocusScore, &actual.SupersedesScanID, &actual.CapturedBy, &at); err != nil {
			return err
		}
		item := expected[index]
		if actual.ID != item.ID || actual.CatalogNumber != item.CatalogNumber || actual.Version != item.Version || actual.ContentChecksum != item.ContentChecksum || actual.PixelWidth != item.PixelWidth || actual.PixelHeight != item.PixelHeight || actual.BitDepth != item.BitDepth || actual.ExposureScore != item.ExposureScore || actual.FocusScore != item.FocusScore || actual.SupersedesScanID != item.SupersedesScanID || actual.CapturedBy != item.CapturedBy || at != item.CapturedAt.Format(time.RFC3339Nano) {
			return projectionMismatch(batch.ID, "扫描")
		}
		index++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if index != len(expected) {
		return projectionMismatch(batch.ID, "扫描")
	}
	return nil
}

func (s *Store) validateIssueProjection(ctx context.Context, batch *domain.PlateBatch) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,scan_id,rule_code,status,resolution_kind,replacement_scan_id,resolved_by,resolved_at,resolution_history FROM quality_issues WHERE batch_id=? ORDER BY id`, batch.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	expected := append([]domain.QualityIssue(nil), batch.Issues...)
	sort.Slice(expected, func(i, j int) bool { return expected[i].ID < expected[j].ID })
	index := 0
	for rows.Next() {
		if index >= len(expected) {
			return projectionMismatch(batch.ID, "质量问题")
		}
		var id, scanID, rule, status, kind, replacement, resolvedBy, resolvedAt string
		var historyRaw []byte
		if err = rows.Scan(&id, &scanID, &rule, &status, &kind, &replacement, &resolvedBy, &resolvedAt, &historyRaw); err != nil {
			return err
		}
		item := expected[index]
		expectedAt := ""
		if item.ResolvedAt != nil {
			expectedAt = item.ResolvedAt.Format(time.RFC3339Nano)
		}
		history := item.ResolutionHistory
		if history == nil {
			history = []domain.IssueResolutionRecord{}
		}
		wantHistory, _ := json.Marshal(history)
		if id != item.ID || scanID != item.ScanID || rule != item.RuleCode || status != item.Status || kind != item.ResolutionKind || replacement != item.ReplacementScanID || resolvedBy != item.ResolvedBy || resolvedAt != expectedAt || !bytes.Equal(historyRaw, wantHistory) {
			return projectionMismatch(batch.ID, "质量问题")
		}
		index++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if index != len(expected) {
		return projectionMismatch(batch.ID, "质量问题")
	}
	return nil
}

func (s *Store) validateConclusionProjection(ctx context.Context, batch *domain.PlateBatch) error {
	rows, err := s.db.QueryContext(ctx, `SELECT scan_id,document FROM quality_conclusions WHERE batch_id=? ORDER BY scan_id`, batch.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	expected := map[string][]byte{}
	for _, item := range batch.QualityConclusions {
		raw, err := json.Marshal(item)
		if err != nil {
			return err
		}
		expected[item.ScanID] = raw
	}
	seen := 0
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return err
		}
		want, ok := expected[id]
		if !ok || !bytes.Equal(raw, want) {
			return projectionMismatch(batch.ID, "质量结论")
		}
		seen++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if seen != len(expected) {
		return projectionMismatch(batch.ID, "质量结论")
	}
	return nil
}

func (s *Store) validatePeerReviewProjection(ctx context.Context, batch *domain.PlateBatch) error {
	rows, err := s.db.QueryContext(ctx, `SELECT ordinal,reviewer,sample_catalogs,passed,note,reviewed_at,evidence FROM peer_reviews WHERE batch_id=? ORDER BY ordinal`, batch.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		if index >= len(batch.PeerReviews) {
			return projectionMismatch(batch.ID, "同行复核")
		}
		var ordinal, passed int
		var reviewer, note, at string
		var samples, evidence []byte
		if err = rows.Scan(&ordinal, &reviewer, &samples, &passed, &note, &at, &evidence); err != nil {
			return err
		}
		item := batch.PeerReviews[index]
		wantSamples, _ := json.Marshal(item.SampleCatalogs)
		evidenceItems := item.Evidence
		if evidenceItems == nil {
			evidenceItems = []domain.PeerReviewEvidence{}
		}
		wantEvidence, _ := json.Marshal(evidenceItems)
		wantPassed := 0
		if item.Passed {
			wantPassed = 1
		}
		if ordinal != index+1 || reviewer != item.Reviewer || !bytes.Equal(samples, wantSamples) || !bytes.Equal(evidence, wantEvidence) || passed != wantPassed || note != item.Note || at != item.ReviewedAt.Format(time.RFC3339Nano) {
			return projectionMismatch(batch.ID, "同行复核")
		}
		index++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if index != len(batch.PeerReviews) {
		return projectionMismatch(batch.ID, "同行复核")
	}
	return nil
}

