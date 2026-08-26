package application

import (
	"astroplate-vault/internal/domain"
)

type CommandResult struct {
	Batch                *domain.PlateBatch             `json:"batch,omitempty"`
	QualitySummary       *domain.QualitySummary         `json:"quality_summary,omitempty"`
	Manifest             *domain.ArchiveManifest        `json:"manifest,omitempty"`
	Replayed             bool                           `json:"replayed"`
	ClosedCount          int                            `json:"closed_count"`
	RemainingOpenCount   int                            `json:"remaining_open_count"`
	CanRequestPeerReview bool                           `json:"can_request_peer_review"`
	RescanResolution     *domain.RescanResolutionResult `json:"rescan_resolution,omitempty"`
	PeerReviewDraft      *PeerReviewDraftView           `json:"peer_review_draft,omitempty"`
}

type PeerReviewDraftView struct {
	Draft            *domain.PeerReviewDraft `json:"draft"`
	BatchRevision    int64                   `json:"batch_revision"`
	CompletedCount   int                     `json:"completed_count"`
	MissingCatalogs  []int                   `json:"missing_catalogs"`
	AllEvidenceValid bool                    `json:"all_evidence_valid"`
}

