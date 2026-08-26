package domain

import (
	"strings"
	"time"
)

const (
	MinResolutionDPI        = 2400
	MaxGrayResponseError    = 0.05
	MaxGeometryErrorPercent = 0.20
	MinPixelWidth           = 4000
	MinPixelHeight          = 4000
	RequiredBitDepth        = 16
	MinExposureScore        = 0.80
	MinFocusScore           = 0.85
)

func NewBatch(id, title string, start, end int, scanner, policy, actor string, now time.Time) (*PlateBatch, error) {
	if err := ValidateIdentifier("id", id); err != nil {
		return nil, err
	}
	if err := ValidateIdentifier("scanner_id", scanner); err != nil {
		return nil, err
	}
	if err := ValidateIdentifier("quality_policy_version", policy); err != nil {
		return nil, err
	}
	if err := ValidatePrincipal("created_by", actor); err != nil {
		return nil, err
	}
	if strings.TrimSpace(title) == "" || len(title) > 200 {
		return nil, NewError(CodeValidation, "批次标题不能为空且不能超过 200 字节")
	}
	if start <= 0 || end < start || end-start > 9999 {
		return nil, NewError(CodeValidation, "底片目录范围无效或超过 10000 张")
	}
	if _, err := ResolvePolicy(policy); err != nil {
		return nil, err
	}
	return &PlateBatch{ID: id, Title: title, CatalogStart: start, CatalogEnd: end, ScannerID: scanner, QualityPolicyVersion: policy, State: StateDraft, Revision: 1, CreatedBy: actor, CreatedAt: now.UTC(), Calibrations: []CalibrationSession{}, Scans: []PlateScan{}, Issues: []QualityIssue{}, QualityConclusions: []ScanQualityConclusion{}, PeerReviews: []PeerReview{}}, nil
}

func (b *PlateBatch) Revise(title string, start, end int, scanner, policy string) ([]FieldChange, error) {
	if b.State != StateDraft || len(b.Scans) > 0 {
		return nil, NewError(CodeInvalidState, "只有尚未通过标定且未进入采集的草稿批次可修订资料")
	}
	for _, c := range b.Calibrations {
		if c.Result == "passed" {
			return nil, NewError(CodeInvalidState, "已出现合格标定的批次不可修订资料")
		}
	}
	candidate, err := NewBatch(b.ID, title, start, end, scanner, policy, b.CreatedBy, b.CreatedAt)
	if err != nil {
		return nil, err
	}
	changes := []FieldChange{}
	add := func(field string, old, next any) {
		if old != next {
			changes = append(changes, FieldChange{Field: field, Old: old, New: next})
		}
	}
	add("title", b.Title, candidate.Title)
	add("catalog_start", b.CatalogStart, candidate.CatalogStart)
	add("catalog_end", b.CatalogEnd, candidate.CatalogEnd)
	add("scanner_id", b.ScannerID, candidate.ScannerID)
	add("quality_policy_version", b.QualityPolicyVersion, candidate.QualityPolicyVersion)
	if len(changes) == 0 {
		return nil, NewError(CodeValidation, "修订未包含任何资料变化")
	}
	b.Title, b.CatalogStart, b.CatalogEnd = candidate.Title, candidate.CatalogStart, candidate.CatalogEnd
	b.ScannerID, b.QualityPolicyVersion = candidate.ScannerID, candidate.QualityPolicyVersion
	b.Revision++
	return changes, nil
}

