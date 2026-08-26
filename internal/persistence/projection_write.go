package persistence

import (
	"context"

	"encoding/json"

	"fmt"

	"time"

	"astroplate-vault/internal/domain"
	_ "modernc.org/sqlite"
)

func (t *Tx) replaceProjection(ctx context.Context, batch *domain.PlateBatch) error {
	for _, table := range []string{"calibration_sessions", "plate_scans", "quality_issues", "quality_conclusions", "peer_reviews"} {
		if _, err := t.tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE batch_id=?`, batch.ID); err != nil {
			return fmt.Errorf("清理 %s 投影: %w", table, err)
		}
	}
	for i := range batch.Calibrations {
		c := &batch.Calibrations[i]
		if _, err := t.tx.ExecContext(ctx, `INSERT INTO calibration_sessions(id,batch_id,resolution_dpi,gray_response_error,geometry_error_percent,performed_by,performed_at,result) VALUES(?,?,?,?,?,?,?,?)`, c.ID, batch.ID, c.ResolutionDPI, c.GrayResponseError, c.GeometryErrorPercent, c.PerformedBy, c.PerformedAt.Format(time.RFC3339Nano), c.Result); err != nil {
			return fmt.Errorf("保存标定投影: %w", err)
		}
	}
	for _, scan := range batch.Scans {
		if _, err := t.tx.ExecContext(ctx, `INSERT INTO plate_scans(id,batch_id,catalog_number,version,content_checksum,pixel_width,pixel_height,bit_depth,exposure_score,focus_score,supersedes_scan_id,captured_by,captured_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, scan.ID, batch.ID, scan.CatalogNumber, scan.Version, scan.ContentChecksum, scan.PixelWidth, scan.PixelHeight, scan.BitDepth, scan.ExposureScore, scan.FocusScore, scan.SupersedesScanID, scan.CapturedBy, scan.CapturedAt.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("保存扫描投影: %w", err)
		}
	}
	for _, issue := range batch.Issues {
		resolvedAt := ""
		if issue.ResolvedAt != nil {
			resolvedAt = issue.ResolvedAt.Format(time.RFC3339Nano)
		}
		history := issue.ResolutionHistory
		if history == nil {
			history = []domain.IssueResolutionRecord{}
		}
		historyRaw, err := json.Marshal(history)
		if err != nil {
			return err
		}
		if _, err := t.tx.ExecContext(ctx, `INSERT INTO quality_issues(id,batch_id,scan_id,rule_code,severity,observed_value,threshold_text,resolution_kind,resolution_note,replacement_scan_id,status,resolved_by,resolved_at,resolution_history) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, issue.ID, batch.ID, issue.ScanID, issue.RuleCode, issue.Severity, issue.ObservedValue, issue.Threshold, issue.ResolutionKind, issue.ResolutionNote, issue.ReplacementScanID, issue.Status, issue.ResolvedBy, resolvedAt, historyRaw); err != nil {
			return fmt.Errorf("保存质量问题投影: %w", err)
		}
	}
	for _, conclusion := range batch.QualityConclusions {
		raw, err := json.Marshal(conclusion)
		if err != nil {
			return err
		}
		passed := 0
		if conclusion.Passed {
			passed = 1
		}
		if _, err = t.tx.ExecContext(ctx, `INSERT INTO quality_conclusions(batch_id,scan_id,catalog_number,passed,document) VALUES(?,?,?,?,?)`, batch.ID, conclusion.ScanID, conclusion.CatalogNumber, passed, raw); err != nil {
			return fmt.Errorf("保存质量结论投影: %w", err)
		}
	}
	for i, review := range batch.PeerReviews {
		samples, err := json.Marshal(review.SampleCatalogs)
		if err != nil {
			return err
		}
		passed := 0
		if review.Passed {
			passed = 1
		}
		evidenceItems := review.Evidence
		if evidenceItems == nil {
			evidenceItems = []domain.PeerReviewEvidence{}
		}
		evidence, err := json.Marshal(evidenceItems)
		if err != nil {
			return err
		}
		if _, err = t.tx.ExecContext(ctx, `INSERT INTO peer_reviews(batch_id,ordinal,reviewer,sample_catalogs,passed,note,reviewed_at,evidence) VALUES(?,?,?,?,?,?,?,?)`, batch.ID, i+1, review.Reviewer, samples, passed, review.Note, review.ReviewedAt.Format(time.RFC3339Nano), evidence); err != nil {
			return fmt.Errorf("保存同行复核投影: %w", err)
		}
	}
	return nil
}

