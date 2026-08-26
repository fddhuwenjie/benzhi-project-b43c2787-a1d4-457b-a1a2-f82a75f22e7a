package domain

import (
	"time"
)

type BatchState string

const (
	StateDraft          BatchState = "draft"
	StateCapturing      BatchState = "capturing"
	StateQualityReview  BatchState = "quality_review"
	StateRemediation    BatchState = "remediation"
	StatePeerReview     BatchState = "peer_review"
	StatePendingArchive BatchState = "pending_archive"
	StateSealed         BatchState = "sealed"
)

type PlateBatch struct {
	ID                   string                  `json:"id"`
	Title                string                  `json:"title"`
	CatalogStart         int                     `json:"catalog_start"`
	CatalogEnd           int                     `json:"catalog_end"`
	ScannerID            string                  `json:"scanner_id"`
	QualityPolicyVersion string                  `json:"quality_policy_version"`
	State                BatchState              `json:"state"`
	Revision             int64                   `json:"revision"`
	CreatedBy            string                  `json:"created_by"`
	CreatedAt            time.Time               `json:"created_at"`
	SealedAt             *time.Time              `json:"sealed_at,omitempty"`
	Calibration          *CalibrationSession     `json:"calibration,omitempty"`
	Calibrations         []CalibrationSession    `json:"calibrations"`
	Scans                []PlateScan             `json:"scans"`
	Issues               []QualityIssue          `json:"issues"`
	QualityConclusions   []ScanQualityConclusion `json:"quality_conclusions"`
	PeerReviews          []PeerReview            `json:"peer_reviews"`
}

type CalibrationSession struct {
	ID                   string    `json:"id"`
	BatchID              string    `json:"batch_id"`
	ResolutionDPI        int       `json:"resolution_dpi"`
	GrayResponseError    float64   `json:"gray_response_error"`
	GeometryErrorPercent float64   `json:"geometry_error_percent"`
	PerformedBy          string    `json:"performed_by"`
	PerformedAt          time.Time `json:"performed_at"`
	Result               string    `json:"result"`
}

type FieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

type PlateScan struct {
	ID               string    `json:"id"`
	BatchID          string    `json:"batch_id"`
	CatalogNumber    int       `json:"catalog_number"`
	Version          int       `json:"version"`
	ContentChecksum  string    `json:"content_checksum"`
	PixelWidth       int       `json:"pixel_width"`
	PixelHeight      int       `json:"pixel_height"`
	BitDepth         int       `json:"bit_depth"`
	ExposureScore    float64   `json:"exposure_score"`
	FocusScore       float64   `json:"focus_score"`
	SupersedesScanID string    `json:"supersedes_scan_id,omitempty"`
	CapturedBy       string    `json:"captured_by"`
	CapturedAt       time.Time `json:"captured_at"`
}

