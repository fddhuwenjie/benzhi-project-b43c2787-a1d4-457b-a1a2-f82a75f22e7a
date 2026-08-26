package domain

import (
	"encoding/json"
	"time"
)

type IssueResolution struct {
	IssueID           string `json:"issue_id"`
	ResolutionKind    string `json:"resolution_kind"`
	ResolutionNote    string `json:"resolution_note"`
	ReplacementScanID string `json:"replacement_scan_id,omitempty"`
}

type ManifestEntry struct {
	CatalogNumber    int    `json:"catalog_number"`
	ScanID           string `json:"scan_id"`
	Version          int    `json:"version"`
	ContentChecksum  string `json:"content_checksum"`
	PixelWidth       int    `json:"pixel_width"`
	PixelHeight      int    `json:"pixel_height"`
	BitDepth         int    `json:"bit_depth"`
	SupersedesScanID string `json:"supersedes_scan_id,omitempty"`
}

type ArchiveManifest struct {
	BatchID       string          `json:"batch_id"`
	BatchRevision int64           `json:"batch_revision"`
	Entries       []ManifestEntry `json:"entries"`
	AuditHeadHash string          `json:"audit_head_hash"`
	ManifestHash  string          `json:"manifest_hash"`
	SealedBy      string          `json:"sealed_by"`
	SealedAt      time.Time       `json:"sealed_at"`
}

type QualitySummary struct {
	Total      int `json:"total"`
	Passed     int `json:"passed"`
	Failed     int `json:"failed"`
	IssueCount int `json:"issue_count"`
}

type MetricConclusion struct {
	RuleCode      string  `json:"rule_code"`
	ObservedValue float64 `json:"observed_value"`
	Threshold     string  `json:"threshold"`
	Passed        bool    `json:"passed"`
}

type ScanQualityConclusion struct {
	ScanID        string             `json:"scan_id"`
	CatalogNumber int                `json:"catalog_number"`
	Passed        bool               `json:"passed"`
	Metrics       []MetricConclusion `json:"metrics"`
}

type AuditEvent struct {
	BatchID      string          `json:"batch_id"`
	Sequence     int64           `json:"sequence"`
	EventType    string          `json:"event_type"`
	Revision     int64           `json:"revision"`
	Actor        string          `json:"actor"`
	Payload      json.RawMessage `json:"payload"`
	OccurredAt   time.Time       `json:"occurred_at"`
	PreviousHash string          `json:"previous_hash"`
	EventHash    string          `json:"event_hash"`
}

