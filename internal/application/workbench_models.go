package application

import (
	"time"

	"astroplate-vault/internal/domain"
)

type BatchWorkbenchQuery struct {
	State, ScannerID, QualityPolicyVersion, CreatedBy, Title, Cursor string
	Limit                                                            int
}

type BatchWorkbenchItem struct {
	BatchID              string            `json:"batch_id"`
	Title                string            `json:"title"`
	State                domain.BatchState `json:"state"`
	Revision             int64             `json:"revision"`
	ScannerID            string            `json:"scanner_id"`
	QualityPolicyVersion string            `json:"quality_policy_version"`
	CreatedBy            string            `json:"created_by"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
	CatalogCount         int               `json:"catalog_count"`
	ActiveScanCount      int               `json:"active_scan_count"`
	MissingCatalogCount  int               `json:"missing_catalog_count"`
	OpenIssueCount       int               `json:"open_issue_count"`
	NextAction           string            `json:"next_action"`
}

type BatchStateCount struct {
	State domain.BatchState `json:"state"`
	Count int               `json:"count"`
}

type BatchWorkbench struct {
	Items       []BatchWorkbenchItem `json:"items"`
	Total       int                  `json:"total"`
	StateCounts []BatchStateCount    `json:"state_counts"`
	NextCursor  string               `json:"next_cursor,omitempty"`
}

type AuditPage struct {
	Events               []domain.AuditEvent `json:"events"`
	HeadHash             string              `json:"head_hash"`
	NextAfter            int64               `json:"next_after"`
	Verified             bool                `json:"verified"`
	FirstInvalidSequence int64               `json:"first_invalid_sequence,omitempty"`
	IntegrityDetail      string              `json:"integrity_detail,omitempty"`
	MatchedCount         int                 `json:"matched_count"`
	SummaryByEventType   []AuditGroup        `json:"summary_by_event_type"`
	SummaryByActor       []AuditGroup        `json:"summary_by_actor"`
	Summary              AuditSummary        `json:"summary"`
}
type AuditGroup struct {
	Key           string `json:"key"`
	Count         int    `json:"count"`
	FirstSequence int64  `json:"first_sequence"`
	LastSequence  int64  `json:"last_sequence"`
	FirstRevision int64  `json:"first_revision"`
	LastRevision  int64  `json:"last_revision"`
	EventType     string `json:"event_type,omitempty"`
	Actor         string `json:"actor,omitempty"`
}
type AuditSummary struct {
	MatchedCount int          `json:"matched_count"`
	ByEventType  []AuditGroup `json:"by_event_type"`
	ByActor      []AuditGroup `json:"by_actor"`
}

