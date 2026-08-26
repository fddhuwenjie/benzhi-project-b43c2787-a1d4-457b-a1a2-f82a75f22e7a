package domain

import (
	"time"
)

type PeerReviewDraftStatus string

const (
	PeerReviewDraftOpen      PeerReviewDraftStatus = "open"
	PeerReviewDraftCompleted PeerReviewDraftStatus = "completed"
)

// PeerReviewDraft 将一轮抽验的固定样本与分段提交的证据持久化，独立于批次 revision 演进。
type PeerReviewDraft struct {
	ID                string                    `json:"id"`
	BatchID           string                    `json:"batch_id"`
	Round             int                       `json:"round"`
	Reviewer          string                    `json:"reviewer"`
	BaseBatchRevision int64                     `json:"base_batch_revision"`
	DraftRevision     int64                     `json:"draft_revision"`
	Status            PeerReviewDraftStatus     `json:"status"`
	Samples           []PeerReviewDraftSample   `json:"samples"`
	Evidence          []PeerReviewDraftEvidence `json:"evidence"`
	CreatedAt         time.Time                 `json:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`
	CompletedAt       *time.Time                `json:"completed_at,omitempty"`
}

type PeerReviewDraftSample struct {
	CatalogNumber   int    `json:"catalog_number"`
	ScanID          string `json:"scan_id"`
	Version         int    `json:"version"`
	ContentChecksum string `json:"content_checksum"`
}

type PeerReviewDraftEvidence struct {
	CatalogNumber    int    `json:"catalog_number"`
	ScanID           string `json:"scan_id"`
	Version          int    `json:"version"`
	ObservedChecksum string `json:"observed_checksum"`
	ChecksumMatch    bool   `json:"checksum_match"`
	DimensionsMatch  bool   `json:"dimensions_match"`
	BitDepthMatch    bool   `json:"bit_depth_match"`
	Note             string `json:"note,omitempty"`
}

