package domain

import (
	"time"
)

type QualityIssue struct {
	ID                string                  `json:"id"`
	BatchID           string                  `json:"batch_id"`
	ScanID            string                  `json:"scan_id"`
	RuleCode          string                  `json:"rule_code"`
	Severity          string                  `json:"severity"`
	ObservedValue     float64                 `json:"observed_value"`
	Threshold         string                  `json:"threshold"`
	ResolutionKind    string                  `json:"resolution_kind,omitempty"`
	ResolutionNote    string                  `json:"resolution_note,omitempty"`
	ReplacementScanID string                  `json:"replacement_scan_id,omitempty"`
	Status            string                  `json:"status"`
	ResolvedBy        string                  `json:"resolved_by,omitempty"`
	ResolvedAt        *time.Time              `json:"resolved_at,omitempty"`
	ResolutionHistory []IssueResolutionRecord `json:"resolution_history"`
}

type IssueResolutionRecord struct {
	ResolutionKind    string     `json:"resolution_kind"`
	ResolutionNote    string     `json:"resolution_note"`
	ReplacementScanID string     `json:"replacement_scan_id,omitempty"`
	ResolvedBy        string     `json:"resolved_by"`
	ResolvedAt        time.Time  `json:"resolved_at"`
	RevokedReason     string     `json:"revoked_reason,omitempty"`
	RevokedBy         string     `json:"revoked_by,omitempty"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
}

