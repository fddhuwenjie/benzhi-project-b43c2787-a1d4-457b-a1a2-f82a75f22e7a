package application

import (
	"context"
	"sort"

	"astroplate-vault/internal/audit"
	"astroplate-vault/internal/domain"
)

func (s *Service) CorrectScan(ctx context.Context, batchID, scanID string, r ScanCorrectionRequest) (CommandResult, error) {
	return s.mutate(ctx, batchID, "correct_scan:"+scanID, "scan.corrected", r.CommandMeta, r, func(b *domain.PlateBatch) (any, error) {
		next := domain.PlateScan{ID: newID("scan"), CatalogNumber: r.CatalogNumber, ContentChecksum: r.ContentChecksum, PixelWidth: r.PixelWidth, PixelHeight: r.PixelHeight, BitDepth: r.BitDepth, ExposureScore: r.ExposureScore, FocusScore: r.FocusScore, CapturedBy: r.Actor, CapturedAt: s.now()}
		changes, err := b.CorrectScan(scanID, next, r.Reason)
		if err != nil {
			return nil, err
		}
		created := b.Scans[len(b.Scans)-1]
		return map[string]any{"old_scan_id": scanID, "new_scan_id": created.ID, "version": created.Version, "changes": changes, "reason": r.Reason}, nil
	})
}

func qualityRules(conclusions []domain.ScanQualityConclusion) ([]RuleStatistic, []int) {
	byRule := map[string]*RuleStatistic{}
	failedSet := map[int]bool{}
	for _, c := range conclusions {
		for _, m := range c.Metrics {
			x := byRule[m.RuleCode]
			if x == nil {
				x = &RuleStatistic{RuleCode: m.RuleCode, Threshold: m.Threshold}
				byRule[m.RuleCode] = x
			}
			if m.Passed {
				x.PassedCount++
			} else {
				x.FailedCount++
				x.FailedCatalogs = append(x.FailedCatalogs, c.CatalogNumber)
				failedSet[c.CatalogNumber] = true
			}
		}
	}
	keys := make([]string, 0, len(byRule))
	for k := range byRule {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rules := make([]RuleStatistic, 0, len(keys))
	for _, k := range keys {
		rules = append(rules, *byRule[k])
	}
	failed := make([]int, 0, len(failedSet))
	for c := range failedSet {
		failed = append(failed, c)
	}
	sort.Ints(failed)
	return rules, failed
}

func (s *Service) QualityPreview(ctx context.Context, batchID string, expected int64) (QualityPreview, error) {
	if expected < 1 {
		return QualityPreview{}, domain.NewError(domain.CodeValidation, "expected_revision 必须为正整数")
	}
	unlock := s.lock(batchID)
	defer unlock()
	b, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return QualityPreview{}, err
	}
	if b.Revision != expected {
		return QualityPreview{}, domain.RevisionConflict(b.Revision)
	}
	if b.State != domain.StateCapturing {
		return QualityPreview{}, domain.NewError(domain.CodeInvalidState, "只有 capturing 状态可预览质量评估")
	}
	missing := domain.MissingCatalogs(b)
	out := QualityPreview{BatchID: b.ID, BatchRevision: b.Revision, ExpectedCount: b.CatalogEnd - b.CatalogStart + 1, CapturedCount: len(b.ActiveScans()), MissingCount: len(missing), MissingCatalogs: missing, Rules: []RuleStatistic{}, FailedCatalogs: []int{}, Conclusions: []domain.ScanQualityConclusion{}}
	if len(missing) > 0 {
		return out, nil
	}
	conclusions, summary, err := b.CalculateQuality()
	if err != nil {
		return QualityPreview{}, err
	}
	out.CanEvaluate = true
	out.Conclusions = conclusions
	out.Summary = summary
	out.ExpectedIssueCount = summary.IssueCount
	out.Rules, out.FailedCatalogs = qualityRules(conclusions)
	return out, nil
}

func (s *Service) RevokeIssueResolution(ctx context.Context, batchID, issueID string, r ResolutionRevocationRequest) (CommandResult, error) {
	return s.mutate(ctx, batchID, "revoke_issue_resolution:"+issueID, "issue.resolution_revoked", r.CommandMeta, r, func(b *domain.PlateBatch) (any, error) {
		record, err := b.RevokeIssueResolution(issueID, r.Reason, r.Actor, s.now())
		if err != nil {
			return nil, err
		}
		return map[string]any{"issue_id": issueID, "revoked_resolution": record, "reason": r.Reason}, nil
	})
}

func (s *Service) PeerReviewWorkItem(ctx context.Context, batchID, reviewer string, expected int64) (PeerReviewWorkItem, error) {
	if expected < 1 {
		return PeerReviewWorkItem{}, domain.NewError(domain.CodeValidation, "expected_revision 必须为正整数")
	}
	if err := domain.ValidatePrincipal("reviewer", reviewer); err != nil {
		return PeerReviewWorkItem{}, err
	}
	unlock := s.lock(batchID)
	defer unlock()
	b, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return PeerReviewWorkItem{}, err
	}
	if b.Revision != expected {
		return PeerReviewWorkItem{}, domain.RevisionConflict(b.Revision)
	}
	if b.State != domain.StatePeerReview {
		return PeerReviewWorkItem{}, domain.NewError(domain.CodeInvalidState, "批次不处于 peer_review 状态")
	}
	out := PeerReviewWorkItem{BatchID: b.ID, BatchRevision: b.Revision, ReviewRound: len(b.PeerReviews) + 1, EvidenceSubmitRevision: b.Revision, Reviewer: reviewer, Eligible: true, Samples: []PeerReviewWorkItemScan{}}
	if reviewer == b.CreatedBy {
		out.Eligible = false
		out.BlockingReason = "复核员是批次创建人"
	}
	for _, sc := range b.Scans {
		if sc.CapturedBy == reviewer {
			out.Eligible = false
			out.BlockingReason = "复核员参与过本批次扫描采集"
			break
		}
	}
	active := map[int]domain.PlateScan{}
	for _, sc := range b.ActiveScans() {
		active[sc.CatalogNumber] = sc
	}
	for _, catalog := range b.SampleCatalogs() {
		sc, ok := active[catalog]
		if !ok {
			return PeerReviewWorkItem{}, domain.NewError(domain.CodeIntegrity, "固定样本目录 %d 缺少活动扫描", catalog)
		}
		out.Samples = append(out.Samples, PeerReviewWorkItemScan{CatalogNumber: catalog, ScanID: sc.ID, Version: sc.Version, ContentChecksum: sc.ContentChecksum, PixelWidth: sc.PixelWidth, PixelHeight: sc.PixelHeight, BitDepth: sc.BitDepth})
	}
	return out, nil
}

func (s *Service) ReconcileManifest(ctx context.Context, batchID string, r ManifestReconcileRequest) (domain.ManifestReconciliation, error) {
	unlock := s.lock(batchID)
	defer unlock()
	b, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return domain.ManifestReconciliation{}, err
	}
	if b.State != domain.StateSealed {
		return domain.ManifestReconciliation{}, domain.NewError(domain.CodeInvalidState, "只有 sealed 批次可核验封存成果")
	}
	if len(r.Entries) > b.CatalogEnd-b.CatalogStart+1 {
		return domain.ManifestReconciliation{}, domain.NewError(domain.CodeValidation, "核验成果条目超过批次规模上限")
	}
	m, err := s.store.LoadManifest(ctx, batchID)
	if err != nil {
		return domain.ManifestReconciliation{}, err
	}
	if r.ManifestHash == "" || r.ManifestHash != m.ManifestHash {
		return domain.ManifestReconciliation{}, domain.NewError(domain.CodeIntegrity, "manifest_hash 与已封存清单不一致")
	}
	if err = domain.ValidateSealedManifest(b, m); err != nil {
		return domain.ManifestReconciliation{}, err
	}
	events, err := s.store.AllAudit(ctx, batchID)
	if err != nil {
		return domain.ManifestReconciliation{}, err
	}
	if err = audit.Verify(events); err != nil {
		return domain.ManifestReconciliation{}, err
	}
	present := false
	for _, e := range events {
		if e.EventHash == m.AuditHeadHash {
			present = true
			break
		}
	}
	if !present {
		return domain.ManifestReconciliation{}, domain.NewError(domain.CodeIntegrity, "清单审计锚点不在批次审计链中")
	}
	return domain.ReconcileManifest(m, r.Entries)
}
