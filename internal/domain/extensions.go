package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type PeerEvidenceInput struct {
	CatalogNumber    int
	ObservedChecksum string
	DimensionsMatch  bool
	BitDepthMatch    bool
	Note             string
}

type RescanResolutionResult struct {
	OldScanID      string             `json:"old_scan_id"`
	NewScanID      string             `json:"new_scan_id"`
	ClosedIssueIDs []string           `json:"closed_issue_ids"`
	RuleResults    []MetricConclusion `json:"rule_results"`
	RemainingOpen  int                `json:"remaining_open_issues"`
	CanPeerReview  bool               `json:"can_request_peer_review"`
}

// ResolveIssuesByRescan 在聚合副本上完成替代、锁定策略复验和问题闭合，成功时仅推进一个 revision。
func (b *PlateBatch) ResolveIssuesByRescan(targetScanID string, replacement PlateScan, actor string, now time.Time) (RescanResolutionResult, error) {
	if b.State != StateRemediation {
		return RescanResolutionResult{}, NewError(CodeInvalidState, "只有 remediation 状态可执行原子重扫整改")
	}
	if err := ValidatePrincipal("actor", actor); err != nil {
		return RescanResolutionResult{}, err
	}
	var target *PlateScan
	for i := range b.Scans {
		if b.Scans[i].ID == targetScanID {
			target = &b.Scans[i]
			break
		}
	}
	if target == nil {
		return RescanResolutionResult{}, NewError(CodeNotFound, "目标扫描不存在")
	}
	active := false
	for _, scan := range b.ActiveScans() {
		if scan.ID == targetScanID {
			active = true
			break
		}
	}
	if !active {
		return RescanResolutionResult{}, NewError(CodeConflict, "目标扫描已被替代")
	}
	policy, err := ResolvePolicy(b.QualityPolicyVersion)
	if err != nil {
		return RescanResolutionResult{}, err
	}
	knownRules := map[string]bool{}
	for _, metric := range policy.Evaluate(*target).Metrics {
		knownRules[metric.RuleCode] = true
	}
	issueIndexes := []int{}
	for i, issue := range b.Issues {
		if issue.ScanID != targetScanID || issue.Status != "open" {
			continue
		}
		if !knownRules[issue.RuleCode] {
			return RescanResolutionResult{}, NewError(CodeValidation, "目标扫描包含非质量策略问题 %s", issue.ID)
		}
		issueIndexes = append(issueIndexes, i)
	}
	if len(issueIndexes) == 0 {
		return RescanResolutionResult{}, NewError(CodeValidation, "目标扫描没有可由重扫闭合的开放质量问题")
	}

	copyBatch := *b
	copyBatch.Scans = append([]PlateScan(nil), b.Scans...)
	copyBatch.Issues = append([]QualityIssue(nil), b.Issues...)
	copyBatch.QualityConclusions = append([]ScanQualityConclusion(nil), b.QualityConclusions...)
	replacement.CatalogNumber = target.CatalogNumber
	replacement.SupersedesScanID = target.ID
	replacement.CapturedBy = actor
	replacement.CapturedAt = now.UTC()
	baseRevision := b.Revision
	if err = copyBatch.AddScan(replacement); err != nil {
		return RescanResolutionResult{}, err
	}
	created := copyBatch.Scans[len(copyBatch.Scans)-1]
	conclusion := policy.Evaluate(created)
	failed := []string{}
	metricByRule := map[string]MetricConclusion{}
	for _, metric := range conclusion.Metrics {
		metricByRule[metric.RuleCode] = metric
		if !metric.Passed {
			failed = append(failed, metric.RuleCode)
		}
	}
	for _, idx := range issueIndexes {
		if metric, ok := metricByRule[copyBatch.Issues[idx].RuleCode]; !ok || !metric.Passed {
			failed = append(failed, copyBatch.Issues[idx].RuleCode)
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		failed = compactStrings(failed)
		return RescanResolutionResult{}, NewError(CodeValidation, "替代扫描复验未通过规则: %s", strings.Join(failed, ","))
	}
	closed := make([]string, 0, len(issueIndexes))
	t := now.UTC()
	for _, idx := range issueIndexes {
		issue := &copyBatch.Issues[idx]
		issue.Status, issue.ResolutionKind, issue.ReplacementScanID = "closed", "rescanned", created.ID
		issue.ResolutionNote, issue.ResolvedBy, issue.ResolvedAt = "原子重扫复验通过", actor, &t
		issue.ResolutionHistory = append(issue.ResolutionHistory, IssueResolutionRecord{ResolutionKind: "rescanned", ResolutionNote: issue.ResolutionNote, ReplacementScanID: created.ID, ResolvedBy: actor, ResolvedAt: t})
		closed = append(closed, issue.ID)
	}
	updated := false
	for i := range copyBatch.QualityConclusions {
		if copyBatch.QualityConclusions[i].CatalogNumber == created.CatalogNumber {
			copyBatch.QualityConclusions[i] = conclusion
			updated = true
			break
		}
	}
	if !updated {
		copyBatch.QualityConclusions = append(copyBatch.QualityConclusions, conclusion)
	}
	copyBatch.Revision = baseRevision + 1
	*b = copyBatch
	sort.Strings(closed)
	remaining := OpenIssueCount(b)
	return RescanResolutionResult{OldScanID: targetScanID, NewScanID: created.ID, ClosedIssueIDs: closed, RuleResults: conclusion.Metrics, RemainingOpen: remaining, CanPeerReview: remaining == 0}, nil
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

// RecordPeerReviewEvidence 只依据聚合锁内取得的固定样本和活动扫描派生本轮结论。
func (b *PlateBatch) RecordPeerReviewEvidence(reviewer string, inputs []PeerEvidenceInput, issueID func() string, now time.Time) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.State != StatePeerReview {
		return NewError(CodeInvalidState, "只有同行抽验状态可提交逐样本证据")
	}
	if err := ValidatePrincipal("reviewer", reviewer); err != nil {
		return err
	}
	if reviewer == b.CreatedBy {
		return NewError(CodeForbidden, "复核员不能是批次创建人")
	}
	for _, scan := range b.Scans {
		if scan.CapturedBy == reviewer {
			return NewError(CodeForbidden, "复核员不能参与本批次扫描采集")
		}
	}
	expected := b.SampleCatalogs()
	if len(inputs) != len(expected) {
		return NewError(CodeValidation, "逐样本证据必须完整覆盖系统固定样本")
	}
	active := map[int]PlateScan{}
	for _, scan := range b.ActiveScans() {
		active[scan.CatalogNumber] = scan
	}
	seen := map[int]bool{}
	evidence := make([]PeerReviewEvidence, 0, len(inputs))
	passed := true
	for _, input := range inputs {
		if seen[input.CatalogNumber] {
			return NewError(CodeDuplicate, "逐样本证据包含重复目录号")
		}
		seen[input.CatalogNumber] = true
		if err := ValidateChecksum(input.ObservedChecksum); err != nil {
			return err
		}
		if len(input.Note) > 2000 {
			return NewError(CodeValidation, "evidence note 不能超过 2000 字节")
		}
		scan, ok := active[input.CatalogNumber]
		if !ok {
			return NewError(CodeValidation, "样本目录 %d 没有活动扫描", input.CatalogNumber)
		}
		match := input.ObservedChecksum == scan.ContentChecksum
		itemPassed := match && input.DimensionsMatch && input.BitDepthMatch
		if !itemPassed {
			passed = false
		}
		evidence = append(evidence, PeerReviewEvidence{CatalogNumber: input.CatalogNumber, ScanID: scan.ID, Version: scan.Version, ObservedChecksum: input.ObservedChecksum, ChecksumMatch: match, DimensionsMatch: input.DimensionsMatch, BitDepthMatch: input.BitDepthMatch, Note: input.Note})
	}
	for _, catalog := range expected {
		if !seen[catalog] {
			return NewError(CodeValidation, "逐样本证据缺少固定样本目录 %d", catalog)
		}
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].CatalogNumber < evidence[j].CatalogNumber })
	review := PeerReview{Reviewer: reviewer, SampleCatalogs: expected, Passed: passed, ReviewedAt: now.UTC(), Evidence: evidence}
	b.PeerReviews = append(b.PeerReviews, review)
	if passed {
		b.State = StatePendingArchive
	} else {
		b.State = StateRemediation
		for _, e := range evidence {
			if e.ChecksumMatch && e.DimensionsMatch && e.BitDepthMatch {
				continue
			}
			note := strings.TrimSpace(e.Note)
			if note == "" {
				note = "同行抽验逐样本证据不匹配"
			}
			b.Issues = append(b.Issues, QualityIssue{ID: issueID(), BatchID: b.ID, ScanID: e.ScanID, RuleCode: "peer_review_failure", Severity: "error", Threshold: "checksum, dimensions and bit depth match", ResolutionNote: note, Status: "open"})
		}
	}
	b.Revision++
	return nil
}

func ValidateScanLineage(b *PlateBatch) error {
	_, err := validateScanInvariants(b)
	return err
}

func CalibrationFailureReasons(c CalibrationSession) []string {
	reasons := []string{}
	if c.ResolutionDPI < MinResolutionDPI {
		reasons = append(reasons, "分辨率未达到阈值")
	}
	if c.GrayResponseError > MaxGrayResponseError {
		reasons = append(reasons, "灰阶响应误差超过阈值")
	}
	if c.GeometryErrorPercent > MaxGeometryErrorPercent {
		reasons = append(reasons, "几何偏差超过阈值")
	}
	return reasons
}

func EnsureArchiveBusinessReady(b *PlateBatch) []string {
	codes := []string{}
	if b.State != StatePendingArchive {
		codes = append(codes, "batch_state")
	}
	if len(MissingCatalogs(b)) != 0 {
		codes = append(codes, "catalog_completeness")
	}
	if OpenIssueCount(b) != 0 {
		codes = append(codes, "open_quality_issues")
	}
	if len(b.PeerReviews) == 0 || !b.PeerReviews[len(b.PeerReviews)-1].Passed {
		codes = append(codes, "latest_peer_review")
	}
	if len(b.PeerReviews) > 0 {
		reviewer := b.PeerReviews[len(b.PeerReviews)-1].Reviewer
		if reviewer == b.CreatedBy {
			codes = append(codes, "duty_separation")
		} else {
			for _, scan := range b.Scans {
				if scan.CapturedBy == reviewer {
					codes = append(codes, "duty_separation")
					break
				}
			}
		}
	}
	sort.Strings(codes)
	return codes
}

func DescribeBlocker(code string, b *PlateBatch) string {
	switch code {
	case "batch_state":
		return fmt.Sprintf("批次状态 %s 不是 pending_archive", b.State)
	case "catalog_completeness":
		return fmt.Sprintf("仍缺少 %d 个目录", len(MissingCatalogs(b)))
	case "open_quality_issues":
		return fmt.Sprintf("仍有 %d 个开放质量问题", OpenIssueCount(b))
	case "latest_peer_review":
		return "最新同行抽验未通过或不存在"
	case "duty_separation":
		return "最新复核员不满足职责分离"
	default:
		return code
	}
}
