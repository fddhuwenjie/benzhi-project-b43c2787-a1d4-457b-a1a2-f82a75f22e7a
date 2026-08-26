package application

type CommandMeta struct {
	RequestID        string `json:"request_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Actor            string `json:"actor"`
}

type CreateBatchRequest struct {
	CommandMeta
	ID                   string `json:"id,omitempty"`
	Title                string `json:"title"`
	CatalogStart         int    `json:"catalog_start"`
	CatalogEnd           int    `json:"catalog_end"`
	ScannerID            string `json:"scanner_id"`
	QualityPolicyVersion string `json:"quality_policy_version"`
}

type CalibrationRequest struct {
	CommandMeta
	CalibrationID        string  `json:"calibration_id,omitempty"`
	SessionID            string  `json:"session_id,omitempty"`
	ResolutionDPI        int     `json:"resolution_dpi"`
	GrayResponseError    float64 `json:"gray_response_error"`
	GeometryErrorPercent float64 `json:"geometry_error_percent"`
}

type ReviseBatchRequest struct {
	CommandMeta
	Title                *string `json:"title,omitempty"`
	CatalogStart         *int    `json:"catalog_start,omitempty"`
	CatalogEnd           *int    `json:"catalog_end,omitempty"`
	ScannerID            *string `json:"scanner_id,omitempty"`
	QualityPolicyVersion *string `json:"quality_policy_version,omitempty"`
}

type ScanRequest struct {
	CommandMeta
	CatalogNumber    int     `json:"catalog_number"`
	ContentChecksum  string  `json:"content_checksum"`
	PixelWidth       int     `json:"pixel_width"`
	PixelHeight      int     `json:"pixel_height"`
	BitDepth         int     `json:"bit_depth"`
	ExposureScore    float64 `json:"exposure_score"`
	FocusScore       float64 `json:"focus_score"`
	SupersedesScanID string  `json:"supersedes_scan_id,omitempty"`
}

type RescanResolutionRequest struct {
	CommandMeta
	TargetScanID    string  `json:"target_scan_id"`
	ContentChecksum string  `json:"content_checksum"`
	PixelWidth      int     `json:"pixel_width"`
	PixelHeight     int     `json:"pixel_height"`
	BitDepth        int     `json:"bit_depth"`
	ExposureScore   float64 `json:"exposure_score"`
	FocusScore      float64 `json:"focus_score"`
}

type ScanCorrectionRequest struct {
	CommandMeta
	CatalogNumber   int     `json:"catalog_number"`
	ContentChecksum string  `json:"content_checksum"`
	PixelWidth      int     `json:"pixel_width"`
	PixelHeight     int     `json:"pixel_height"`
	BitDepth        int     `json:"bit_depth"`
	ExposureScore   float64 `json:"exposure_score"`
	FocusScore      float64 `json:"focus_score"`
	Reason          string  `json:"reason"`
}

type ResolutionRevocationRequest struct {
	CommandMeta
	Reason string `json:"reason"`
}

type BatchScanRequest struct {
	CommandMeta
	Scans []BatchScanItem `json:"scans"`
}
type BatchScanItem struct {
	CatalogNumber    int     `json:"catalog_number"`
	ContentChecksum  string  `json:"content_checksum"`
	PixelWidth       int     `json:"pixel_width"`
	PixelHeight      int     `json:"pixel_height"`
	BitDepth         int     `json:"bit_depth"`
	ExposureScore    float64 `json:"exposure_score"`
	FocusScore       float64 `json:"focus_score"`
	SupersedesScanID string  `json:"supersedes_scan_id,omitempty"`
}
type CatalogPrecheckRequest struct {
	RequestID        string                `json:"request_id"`
	ExpectedRevision int64                 `json:"expected_revision"`
	Catalogs         []CatalogPrecheckItem `json:"catalogs"`
}
type CatalogPrecheckItem struct {
	CatalogNumber   int    `json:"catalog_number"`
	ContentChecksum string `json:"content_checksum"`
}

type EvaluateRequest struct{ CommandMeta }

type ResolveIssueRequest struct {
	CommandMeta
	ResolutionKind    string `json:"resolution_kind"`
	ResolutionNote    string `json:"resolution_note"`
	ReplacementScanID string `json:"replacement_scan_id,omitempty"`
}

type BatchIssueResolutionRequest struct {
	CommandMeta
	Resolutions []BatchIssueResolutionItem `json:"resolutions"`
}
type BatchIssueResolutionItem struct {
	IssueID           string `json:"issue_id"`
	ResolutionKind    string `json:"resolution_kind"`
	ResolutionNote    string `json:"resolution_note"`
	ReplacementScanID string `json:"replacement_scan_id,omitempty"`
}

type PeerReviewRequestRequest struct{ CommandMeta }

type PeerReviewRequest struct {
	CommandMeta
	SampleCatalogs []int  `json:"sample_catalogs"`
	Passed         bool   `json:"passed"`
	Note           string `json:"note,omitempty"`
}

type PeerReviewEvidenceRequest struct {
	CommandMeta
	Evidence []PeerReviewEvidenceItem `json:"evidence"`
}
type PeerReviewEvidenceItem struct {
	CatalogNumber    int    `json:"catalog_number"`
	ObservedChecksum string `json:"observed_checksum"`
	DimensionsMatch  bool   `json:"dimensions_match"`
	BitDepthMatch    bool   `json:"bit_depth_match"`
	Note             string `json:"note,omitempty"`
}

type CreatePeerReviewDraftRequest struct{ CommandMeta }

type PutPeerReviewDraftEvidenceRequest struct {
	CommandMeta
	ExpectedDraftRevision int64  `json:"expected_draft_revision"`
	CatalogNumber         int    `json:"catalog_number"`
	ScanID                string `json:"scan_id"`
	Version               int    `json:"version"`
	ObservedChecksum      string `json:"observed_checksum"`
	DimensionsMatch       bool   `json:"dimensions_match"`
	BitDepthMatch         bool   `json:"bit_depth_match"`
	Note                  string `json:"note,omitempty"`
}

type CompletePeerReviewDraftRequest struct {
	CommandMeta
	ExpectedDraftRevision int64 `json:"expected_draft_revision"`
}

type SealRequest struct{ CommandMeta }

