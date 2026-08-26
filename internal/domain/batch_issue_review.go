package domain

import (
	"crypto/sha256"

	"fmt"
	"sort"
	"strings"
	"time"
)

func (b *PlateBatch) ResolveIssue(issueID, kind, note, replacementID, actor string, now time.Time) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.State != StateRemediation {
		return NewError(CodeInvalidState, "只有整改状态可裁决质量问题")
	}
	if actor == "" {
		return NewError(CodeValidation, "裁决人不能为空")
	}
	if err := ValidatePrincipal("resolved_by", actor); err != nil {
		return err
	}
	if len(note) > 2000 {
		return NewError(CodeValidation, "resolution_note 不能超过 2000 字节")
	}
	var issue *QualityIssue
	for i := range b.Issues {
		if b.Issues[i].ID == issueID {
			issue = &b.Issues[i]
			break
		}
	}
	if issue == nil {
		return NewError(CodeNotFound, "质量问题不存在")
	}
	if issue.Status == "closed" {
		return NewError(CodeDuplicate, "质量问题已关闭")
	}
	if kind != "accepted" && kind != "rescanned" {
		return NewError(CodeValidation, "resolution_kind 必须为 accepted 或 rescanned")
	}
	if kind == "accepted" && strings.TrimSpace(note) == "" {
		return NewError(CodeValidation, "接受质量问题必须填写裁决说明")
	}
	if kind == "rescanned" {
		var replacement *PlateScan
		for i := range b.Scans {
			if b.Scans[i].ID == replacementID {
				replacement = &b.Scans[i]
				break
			}
		}
		if replacement == nil || replacement.SupersedesScanID != issue.ScanID {
			return NewError(CodeValidation, "替代扫描未建立到问题扫描的直接谱系")
		}
		for _, active := range b.ActiveScans() {
			if active.CatalogNumber == replacement.CatalogNumber && active.ID != replacement.ID {
				return NewError(CodeConflict, "替代扫描不是该目录的当前活动版本")
			}
		}
		policy, err := ResolvePolicy(b.QualityPolicyVersion)
		if err != nil {
			return err
		}
		conclusion := policy.Evaluate(*replacement)
		metricPassed := false
		for _, metric := range conclusion.Metrics {
			if metric.RuleCode == issue.RuleCode {
				metricPassed = metric.Passed
				break
			}
		}
		if !metricPassed {
			return NewError(CodeValidation, "替代扫描仍未通过规则 %s", issue.RuleCode)
		}
		updated := false
		for i, existing := range b.QualityConclusions {
			if existing.CatalogNumber == replacement.CatalogNumber {
				b.QualityConclusions[i] = conclusion
				updated = true
				break
			}
		}
		if !updated {
			b.QualityConclusions = append(b.QualityConclusions, conclusion)
		}
		issue.ReplacementScanID = replacementID
	}
	t := now.UTC()
	issue.ResolutionKind = kind
	issue.ResolutionNote = note
	issue.ResolvedBy = actor
	issue.ResolvedAt = &t
	issue.Status = "closed"
	issue.ResolutionHistory = append(issue.ResolutionHistory, IssueResolutionRecord{ResolutionKind: kind, ResolutionNote: note, ReplacementScanID: issue.ReplacementScanID, ResolvedBy: actor, ResolvedAt: t})
	b.Revision++
	return nil
}

func (b *PlateBatch) RequestPeerReview() error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.State != StateQualityReview && b.State != StateRemediation {
		return NewError(CodeInvalidState, "当前状态不可申请同行抽验")
	}
	for _, issue := range b.Issues {
		if issue.Status != "closed" {
			return NewError(CodeValidation, "仍有未关闭的质量问题")
		}
	}
	b.State = StatePeerReview
	b.Revision++
	return nil
}

func (b *PlateBatch) SampleCatalogs() []int {
	active := b.ActiveScans()
	if len(active) == 0 {
		return []int{}
	}
	n := (len(active) + 4) / 5
	if n < 1 {
		n = 1
	}
	seed := sha256.Sum256([]byte(b.ID + ":" + b.QualityPolicyVersion))
	offset := int(seed[0]) % len(active)
	seen := map[int]bool{}
	out := make([]int, 0, n)
	step := len(active) / n
	if step < 1 {
		step = 1
	}
	for i := 0; len(out) < n; i++ {
		idx := (offset + i*step) % len(active)
		c := active[idx].CatalogNumber
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Ints(out)
	return out
}

func (b *PlateBatch) RecordPeerReview(reviewer string, catalogs []int, passed bool, note, failureIssueID string, now time.Time) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.State != StatePeerReview {
		return NewError(CodeInvalidState, "当前状态不可提交同行抽验")
	}
	if reviewer == "" || reviewer == b.CreatedBy {
		return NewError(CodeForbidden, "复核员必须不同于批次采集责任人")
	}
	if err := ValidatePrincipal("reviewer", reviewer); err != nil {
		return err
	}
	if len(note) > 2000 {
		return NewError(CodeValidation, "note 不能超过 2000 字节")
	}
	if !passed {
		if err := ValidateIdentifier("failure_issue_id", failureIssueID); err != nil {
			return err
		}
	}
	for _, scan := range b.Scans {
		if scan.CapturedBy == reviewer {
			return NewError(CodeForbidden, "复核员不能参与本批次扫描采集")
		}
	}
	expected := b.SampleCatalogs()
	sorted := append([]int(nil), catalogs...)
	sort.Ints(sorted)
	if fmt.Sprint(expected) != fmt.Sprint(sorted) {
		return NewError(CodeValidation, "抽验目录必须精确匹配系统固定样本")
	}
	if !passed && strings.TrimSpace(note) == "" {
		return NewError(CodeValidation, "抽验失败必须填写说明")
	}
	b.PeerReviews = append(b.PeerReviews, PeerReview{Reviewer: reviewer, SampleCatalogs: expected, Passed: passed, Note: note, ReviewedAt: now.UTC()})
	if passed {
		b.State = StatePendingArchive
	} else {
		b.State = StateRemediation
		active := b.ActiveScans()
		if len(active) > 0 {
			b.Issues = append(b.Issues, QualityIssue{ID: failureIssueID, BatchID: b.ID, ScanID: active[0].ID, RuleCode: "peer_review_failure", Severity: "error", ObservedValue: 0, Threshold: "passed", ResolutionNote: note, Status: "open"})
		}
	}
	b.Revision++
	return nil
}

