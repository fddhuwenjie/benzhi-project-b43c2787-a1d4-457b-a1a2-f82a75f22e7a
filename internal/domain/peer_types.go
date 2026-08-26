package domain

import (
	"time"
)

type PeerReview struct {
	Reviewer       string               `json:"reviewer"`
	SampleCatalogs []int                `json:"sample_catalogs"`
	Passed         bool                 `json:"passed"`
	Note           string               `json:"note"`
	ReviewedAt     time.Time            `json:"reviewed_at"`
	Evidence       []PeerReviewEvidence `json:"evidence,omitempty"`
}

type PeerReviewEvidence struct {
	CatalogNumber    int    `json:"catalog_number"`
	ScanID           string `json:"scan_id"`
	Version          int    `json:"version"`
	ObservedChecksum string `json:"observed_checksum"`
	ChecksumMatch    bool   `json:"checksum_match"`
	DimensionsMatch  bool   `json:"dimensions_match"`
	BitDepthMatch    bool   `json:"bit_depth_match"`
	Note             string `json:"note,omitempty"`
}

