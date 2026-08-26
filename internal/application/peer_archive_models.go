package application

import (
	"astroplate-vault/internal/domain"
)

type PeerReviewRound struct {
	Ordinal       int               `json:"ordinal"`
	Review        domain.PeerReview `json:"review"`
	SampleMatches bool              `json:"sample_matches"`
	Latest        bool              `json:"latest"`
}
type PeerReviewHistory struct {
	Rounds               []PeerReviewRound `json:"rounds"`
	LatestRound          int               `json:"latest_round"`
	LatestPassed         bool              `json:"latest_passed"`
	RequiresRemediation  bool              `json:"requires_remediation"`
	AllowedNextAction    string            `json:"allowed_next_action"`
	DutySeparationPassed bool              `json:"duty_separation_passed"`
	DutySeparationDetail string            `json:"duty_separation_detail,omitempty"`
}
type PeerReviewWorkItemScan struct {
	CatalogNumber   int    `json:"catalog_number"`
	ScanID          string `json:"scan_id"`
	Version         int    `json:"version"`
	ContentChecksum string `json:"content_checksum"`
	PixelWidth      int    `json:"pixel_width"`
	PixelHeight     int    `json:"pixel_height"`
	BitDepth        int    `json:"bit_depth"`
}
type PeerReviewWorkItem struct {
	BatchID                string                   `json:"batch_id"`
	BatchRevision          int64                    `json:"batch_revision"`
	ReviewRound            int                      `json:"review_round"`
	EvidenceSubmitRevision int64                    `json:"evidence_submit_revision"`
	Reviewer               string                   `json:"reviewer"`
	Eligible               bool                     `json:"eligible"`
	BlockingReason         string                   `json:"blocking_reason,omitempty"`
	Samples                []PeerReviewWorkItemScan `json:"samples"`
}

type ManifestReconcileRequest struct {
	ManifestHash string                       `json:"manifest_hash"`
	Entries      []domain.ManifestActualEntry `json:"entries"`
}
type ManifestPreview struct {
	Manifest         domain.ArchiveManifest `json:"manifest"`
	BatchRevision    int64                  `json:"batch_revision"`
	ExpectedRevision int64                  `json:"expected_revision"`
	AuditHeadHash    string                 `json:"audit_head_hash"`
	ManifestHash     string                 `json:"manifest_hash"`
	ReadOnly         bool                   `json:"read_only"`
}

type ScanVersionProgress struct {
	domain.PlateScan
	Status string `json:"status"`
}
type CatalogProgress struct {
	CatalogNumber int                   `json:"catalog_number"`
	Status        string                `json:"status"`
	Scans         []ScanVersionProgress `json:"scans"`
	ActiveScanID  string                `json:"active_scan_id,omitempty"`
}
type ScanProgress struct {
	BatchID           string            `json:"batch_id"`
	BatchRevision     int64             `json:"batch_revision"`
	ExpectedCount     int               `json:"expected_count"`
	CapturedCount     int               `json:"captured_count"`
	MissingCount      int               `json:"missing_count"`
	ReplacementCount  int               `json:"replacement_count"`
	CompletionPercent float64           `json:"completion_percent"`
	Items             []CatalogProgress `json:"items"`
	NextAfterCatalog  int               `json:"next_after_catalog,omitempty"`
}

type ReadinessBlocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}
type ArchiveReadiness struct {
	BatchID          string             `json:"batch_id"`
	ExpectedRevision int64              `json:"expected_revision"`
	CurrentRevision  int64              `json:"current_revision"`
	CanSeal          bool               `json:"can_seal"`
	Blockers         []ReadinessBlocker `json:"blockers"`
	ManifestHash     string             `json:"manifest_hash,omitempty"`
	AuditHeadHash    string             `json:"audit_head_hash"`
}

type ManifestVerification struct {
	Valid            bool   `json:"valid"`
	ManifestHash     string `json:"manifest_hash"`
	AuditHeadPresent bool   `json:"audit_head_present"`
}

type BatchProjection struct {
	Batch              *domain.PlateBatch `json:"batch"`
	ExpectedPlateCount int                `json:"expected_plate_count"`
	ActiveScanCount    int                `json:"active_scan_count"`
	MissingCatalogs    []int              `json:"missing_catalogs"`
	OpenIssueCount     int                `json:"open_issue_count"`
	PeerReviewSample   []int              `json:"peer_review_sample,omitempty"`
	Writable           bool               `json:"writable"`
	Description        string             `json:"description"`
}

