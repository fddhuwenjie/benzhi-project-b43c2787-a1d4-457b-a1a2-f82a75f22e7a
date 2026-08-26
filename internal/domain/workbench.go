package domain

import (
	"sort"
	"strings"
	"time"
)

func NextBatchAction(state BatchState, missing, open int) string {
	switch state {
	case StateDraft:
		return "submit_calibration"
	case StateCapturing:
		if missing > 0 {
			return "capture_missing_catalogs"
		}
		return "evaluate_quality"
	case StateQualityReview:
		return "request_peer_review"
	case StateRemediation:
		if open > 0 {
			return "resolve_quality_issues"
		}
		return "request_peer_review"
	case StatePeerReview:
		return "complete_peer_review"
	case StatePendingArchive:
		return "seal_batch"
	case StateSealed:
		return "view_manifest"
	default:
		return ""
	}
}

func NewPeerReviewDraft(id string, b *PlateBatch, reviewer string, now time.Time) (*PeerReviewDraft, error) {
	if b == nil || b.State != StatePeerReview {
		return nil, NewError(CodeInvalidState, "只有 peer_review 状态可创建抽验证据草稿")
	}
	if err := ValidateIdentifier("draft_id", id); err != nil {
		return nil, err
	}
	if err := ValidatePrincipal("reviewer", reviewer); err != nil {
		return nil, err
	}
	if reviewer == b.CreatedBy {
		return nil, NewError(CodeForbidden, "复核员不能是批次创建人")
	}
	for _, scan := range b.Scans {
		if scan.CapturedBy == reviewer {
			return nil, NewError(CodeForbidden, "复核员不能参与本批次扫描采集")
		}
	}
	active := map[int]PlateScan{}
	for _, scan := range b.ActiveScans() {
		active[scan.CatalogNumber] = scan
	}
	samples := make([]PeerReviewDraftSample, 0)
	for _, catalog := range b.SampleCatalogs() {
		scan, ok := active[catalog]
		if !ok {
			return nil, NewError(CodeIntegrity, "固定样本目录 %d 缺少活动扫描", catalog)
		}
		samples = append(samples, PeerReviewDraftSample{CatalogNumber: catalog, ScanID: scan.ID, Version: scan.Version, ContentChecksum: scan.ContentChecksum})
	}
	t := now.UTC()
	return &PeerReviewDraft{ID: id, BatchID: b.ID, Round: len(b.PeerReviews) + 1, Reviewer: reviewer, BaseBatchRevision: b.Revision, DraftRevision: 1, Status: PeerReviewDraftOpen, Samples: samples, Evidence: []PeerReviewDraftEvidence{}, CreatedAt: t, UpdatedAt: t}, nil
}

func ValidatePeerReviewDraft(d *PeerReviewDraft, b *PlateBatch, requireOpen bool) error {
	if d == nil {
		return NewError(CodeIntegrity, "同行抽验草稿为空")
	}
	if b == nil || d.BatchID != b.ID {
		return NewError(CodeIntegrity, "同行抽验草稿与批次身份不一致")
	}
	if err := ValidateIdentifier("draft_id", d.ID); err != nil {
		return integrity(err)
	}
	if err := ValidatePrincipal("reviewer", d.Reviewer); err != nil {
		return integrity(err)
	}
	if d.DraftRevision < 1 || d.Round < 1 || d.CreatedAt.IsZero() || d.UpdatedAt.Before(d.CreatedAt) {
		return NewError(CodeIntegrity, "同行抽验草稿修订、轮次或时间无效")
	}
	if d.Status != PeerReviewDraftOpen && d.Status != PeerReviewDraftCompleted {
		return NewError(CodeIntegrity, "同行抽验草稿状态无效")
	}
	if (d.Status == PeerReviewDraftCompleted && d.CompletedAt == nil) || (d.Status == PeerReviewDraftOpen && d.CompletedAt != nil) {
		return NewError(CodeIntegrity, "同行抽验草稿完成状态与时间不一致")
	}
	if d.Round != len(b.PeerReviews)+1 || d.BaseBatchRevision != b.Revision || b.State != StatePeerReview {
		return NewError(CodeInvalidState, "批次已离开草稿创建时的同行抽验轮次")
	}
	if requireOpen && d.Status != PeerReviewDraftOpen {
		return NewError(CodeInvalidState, "已完成的同行抽验草稿不可写入")
	}
	active := map[int]PlateScan{}
	for _, scan := range b.ActiveScans() {
		active[scan.CatalogNumber] = scan
	}
	expected := b.SampleCatalogs()
	if len(d.Samples) != len(expected) {
		return NewError(CodeIntegrity, "同行抽验草稿固定样本数量不一致")
	}
	sampleCatalogs := map[int]bool{}
	for _, sample := range d.Samples {
		if sampleCatalogs[sample.CatalogNumber] {
			return NewError(CodeIntegrity, "同行抽验草稿包含重复固定样本目录 %d", sample.CatalogNumber)
		}
		sampleCatalogs[sample.CatalogNumber] = true
		scan, ok := active[sample.CatalogNumber]
		if !ok || scan.ID != sample.ScanID || scan.Version != sample.Version || scan.ContentChecksum != sample.ContentChecksum {
			return NewError(CodeInvalidState, "目录 %d 的冻结样本已失效", sample.CatalogNumber)
		}
	}
	for _, catalog := range expected {
		if !sampleCatalogs[catalog] {
			return NewError(CodeIntegrity, "同行抽验草稿缺少固定样本目录 %d", catalog)
		}
	}
	evidenceCatalogs := map[int]bool{}
	for _, evidence := range d.Evidence {
		if evidenceCatalogs[evidence.CatalogNumber] || !sampleCatalogs[evidence.CatalogNumber] {
			return NewError(CodeIntegrity, "同行抽验草稿证据目录重复或不属于固定样本")
		}
		evidenceCatalogs[evidence.CatalogNumber] = true
	}
	return nil
}

func (d *PeerReviewDraft) PutEvidence(catalog int, scanID string, version int, observed string, dimensions, bitDepth bool, note string, now time.Time) (bool, error) {
	if d.Status != PeerReviewDraftOpen {
		return false, NewError(CodeInvalidState, "已完成的同行抽验草稿不可写入")
	}
	if err := ValidateChecksum(observed); err != nil {
		return false, err
	}
	if len(note) > 2000 {
		return false, NewError(CodeValidation, "note 不能超过 2000 字节")
	}
	var sample *PeerReviewDraftSample
	for i := range d.Samples {
		if d.Samples[i].CatalogNumber == catalog {
			sample = &d.Samples[i]
			break
		}
	}
	if sample == nil {
		return false, NewError(CodeValidation, "目录 %d 不属于本轮固定样本", catalog)
	}
	if scanID != sample.ScanID || version != sample.Version {
		return false, NewError(CodeValidation, "证据与目录 %d 的冻结扫描身份不符", catalog)
	}
	next := PeerReviewDraftEvidence{CatalogNumber: catalog, ScanID: scanID, Version: version, ObservedChecksum: observed, ChecksumMatch: observed == sample.ContentChecksum, DimensionsMatch: dimensions, BitDepthMatch: bitDepth, Note: note}
	for i, current := range d.Evidence {
		if current.CatalogNumber != catalog {
			continue
		}
		if current == next {
			return false, nil
		}
		d.Evidence[i] = next
		d.DraftRevision++
		d.UpdatedAt = now.UTC()
		sort.Slice(d.Evidence, func(i, j int) bool { return d.Evidence[i].CatalogNumber < d.Evidence[j].CatalogNumber })
		return true, nil
	}
	d.Evidence = append(d.Evidence, next)
	d.DraftRevision++
	d.UpdatedAt = now.UTC()
	sort.Slice(d.Evidence, func(i, j int) bool { return d.Evidence[i].CatalogNumber < d.Evidence[j].CatalogNumber })
	return true, nil
}

func (d *PeerReviewDraft) MissingCatalogs() []int {
	present := map[int]bool{}
	for _, evidence := range d.Evidence {
		present[evidence.CatalogNumber] = true
	}
	missing := []int{}
	for _, sample := range d.Samples {
		if !present[sample.CatalogNumber] {
			missing = append(missing, sample.CatalogNumber)
		}
	}
	return missing
}

func (d *PeerReviewDraft) Complete(b *PlateBatch, issueID func() string, now time.Time) error {
	if err := ValidatePeerReviewDraft(d, b, true); err != nil {
		return err
	}
	if missing := d.MissingCatalogs(); len(missing) > 0 {
		return NewError(CodeInvalidState, "同行抽验证据尚缺目录: %v", missing)
	}
	inputs := make([]PeerEvidenceInput, 0, len(d.Evidence))
	for _, evidence := range d.Evidence {
		inputs = append(inputs, PeerEvidenceInput{CatalogNumber: evidence.CatalogNumber, ObservedChecksum: evidence.ObservedChecksum, DimensionsMatch: evidence.DimensionsMatch, BitDepthMatch: evidence.BitDepthMatch, Note: strings.TrimSpace(evidence.Note)})
	}
	if err := b.RecordPeerReviewEvidence(d.Reviewer, inputs, issueID, now); err != nil {
		return err
	}
	t := now.UTC()
	d.Status, d.CompletedAt, d.UpdatedAt = PeerReviewDraftCompleted, &t, t
	d.DraftRevision++
	return nil
}
