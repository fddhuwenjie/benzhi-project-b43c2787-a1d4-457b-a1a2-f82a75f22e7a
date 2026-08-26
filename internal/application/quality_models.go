package application

import (
	"astroplate-vault/internal/domain"
)

type MetricGate struct {
	ObservedValue float64 `json:"observed_value"`
	Threshold     string  `json:"threshold"`
	Passed        bool    `json:"passed"`
}
type CalibrationDetail struct {
	BatchID        string                     `json:"batch_id"`
	Calibration    *domain.CalibrationSession `json:"calibration,omitempty"`
	Resolution     MetricGate                 `json:"resolution"`
	GrayResponse   MetricGate                 `json:"gray_response"`
	Geometry       MetricGate                 `json:"geometry"`
	GateResult     string                     `json:"gate_result"`
	FailureReasons []string                   `json:"failure_reasons,omitempty"`
	BatchState     domain.BatchState          `json:"batch_state"`
	Sessions       []CalibrationAttempt       `json:"sessions"`
	CurrentSession *domain.CalibrationSession `json:"current_session,omitempty"`
	AttemptCount   int                        `json:"attempt_count"`
}
type CalibrationAttempt struct {
	Session        domain.CalibrationSession `json:"session"`
	Resolution     MetricGate                `json:"resolution"`
	GrayResponse   MetricGate                `json:"gray_response"`
	Geometry       MetricGate                `json:"geometry"`
	FailureReasons []string                  `json:"failure_reasons,omitempty"`
}
type CatalogPrecheckResult struct {
	CatalogNumber int    `json:"catalog_number"`
	Status        string `json:"status"`
}
type CatalogPrecheckResponse struct {
	BatchID           string                  `json:"batch_id"`
	BatchRevision     int64                   `json:"batch_revision"`
	Items             []CatalogPrecheckResult `json:"items"`
	MissingCatalogs   []int                   `json:"missing_catalogs"`
	AlreadyRegistered []int                   `json:"already_registered"`
	OutOfRange        []int                   `json:"out_of_range"`
}
type RuleStatistic struct {
	RuleCode       string `json:"rule_code"`
	PassedCount    int    `json:"passed_count"`
	FailedCount    int    `json:"failed_count"`
	Threshold      string `json:"threshold"`
	FailedCatalogs []int  `json:"failed_catalogs"`
}
type QualityResult struct {
	Summary     domain.QualitySummary          `json:"summary"`
	Rules       []RuleStatistic                `json:"rules"`
	Conclusions []domain.ScanQualityConclusion `json:"conclusions"`
}
type QualityPreview struct {
	BatchID            string                         `json:"batch_id"`
	BatchRevision      int64                          `json:"batch_revision"`
	CanEvaluate        bool                           `json:"can_evaluate"`
	ExpectedCount      int                            `json:"expected_count"`
	CapturedCount      int                            `json:"captured_count"`
	MissingCount       int                            `json:"missing_count"`
	MissingCatalogs    []int                          `json:"missing_catalogs"`
	ExpectedIssueCount int                            `json:"expected_issue_count"`
	Summary            domain.QualitySummary          `json:"summary"`
	Rules              []RuleStatistic                `json:"rules"`
	FailedCatalogs     []int                          `json:"failed_catalogs"`
	Conclusions        []domain.ScanQualityConclusion `json:"conclusions"`
}
type IssueQueueItem struct {
	Issue                    domain.QualityIssue `json:"issue"`
	CatalogNumber            int                 `json:"catalog_number"`
	ActiveScanID             string              `json:"active_scan_id"`
	ActiveVersion            int                 `json:"active_version"`
	CanResolve               bool                `json:"can_resolve"`
	AvailableResolutionKinds []string            `json:"available_resolution_kinds,omitempty"`
}
type IssueQueue struct {
	Items       []IssueQueueItem `json:"items"`
	OpenCount   int              `json:"open_count"`
	ClosedCount int              `json:"closed_count"`
	AllClosed   bool             `json:"all_closed"`
	NextAfter   string           `json:"next_after,omitempty"`
}

