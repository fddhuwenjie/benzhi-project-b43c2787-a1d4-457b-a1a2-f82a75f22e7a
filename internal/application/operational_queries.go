package application

import (
	"context"

	"fmt"
	"sort"
	"strconv"

	"astroplate-vault/internal/audit"
	"astroplate-vault/internal/domain"
)

func (s *Service) CalibrationDetails(ctx context.Context, batchID string) (CalibrationDetail, error) {
	b, e := s.GetBatch(ctx, batchID)
	if e != nil {
		return CalibrationDetail{}, e
	}
	if b.Calibration == nil {
		return CalibrationDetail{}, domain.NewError(domain.CodeNotCalibrated, "批次尚未标定")
	}
	c := b.Calibration
	d := CalibrationDetail{BatchID: batchID, Calibration: c, BatchState: b.State, GateResult: c.Result}
	d.Resolution = MetricGate{float64(c.ResolutionDPI), fmt.Sprintf(">=%d", domain.MinResolutionDPI), c.ResolutionDPI >= domain.MinResolutionDPI}
	d.GrayResponse = MetricGate{c.GrayResponseError, fmt.Sprintf("<=%.2f", domain.MaxGrayResponseError), c.GrayResponseError <= domain.MaxGrayResponseError}
	d.Geometry = MetricGate{c.GeometryErrorPercent, fmt.Sprintf("<=%.2f", domain.MaxGeometryErrorPercent), c.GeometryErrorPercent <= domain.MaxGeometryErrorPercent}
	if !d.Resolution.Passed {
		d.FailureReasons = append(d.FailureReasons, "分辨率未达到阈值")
	}
	if !d.GrayResponse.Passed {
		d.FailureReasons = append(d.FailureReasons, "灰阶响应误差超过阈值")
	}
	if !d.Geometry.Passed {
		d.FailureReasons = append(d.FailureReasons, "几何偏差超过阈值")
	}
	return d, nil
}

func (s *Service) QualityResult(ctx context.Context, batchID string, rule, passed, catalog string) (QualityResult, error) {
	b, e := s.GetBatch(ctx, batchID)
	if e != nil {
		return QualityResult{}, e
	}
	allowed := b.State == domain.StateQualityReview || b.State == domain.StateRemediation || b.State == domain.StatePeerReview || b.State == domain.StatePendingArchive || b.State == domain.StateSealed
	if !allowed {
		return QualityResult{}, domain.NewError(domain.CodeNotEvaluated, "批次尚未执行质量评估")
	}
	if len(b.QualityConclusions) == 0 {
		return QualityResult{}, domain.NewError(domain.CodeNotEvaluated, "批次尚未执行质量评估")
	}
	if passed != "" && passed != "true" && passed != "false" {
		return QualityResult{}, domain.NewError(domain.CodeValidation, "passed 必须为 true 或 false")
	}
	catalogNumber := 0
	if catalog != "" {
		var err error
		catalogNumber, err = strconv.Atoi(catalog)
		if err != nil || catalogNumber < 1 {
			return QualityResult{}, domain.NewError(domain.CodeValidation, "catalog_number 必须为正整数")
		}
	}
	q := QualityResult{Conclusions: []domain.ScanQualityConclusion{}, Summary: domain.QualitySummary{Total: len(b.QualityConclusions), IssueCount: len(b.Issues)}}
	for _, c := range b.QualityConclusions {
		if c.Passed {
			q.Summary.Passed++
		} else {
			q.Summary.Failed++
		}
		if (passed == "" || passed == fmt.Sprint(c.Passed)) && (catalogNumber == 0 || catalogNumber == c.CatalogNumber) {
			q.Conclusions = append(q.Conclusions, c)
		}
		for _, m := range c.Metrics {
			if rule != "" && m.RuleCode != rule {
				continue
			}
			idx := -1
			for i := range q.Rules {
				if q.Rules[i].RuleCode == m.RuleCode {
					idx = i
					break
				}
			}
			if idx < 0 {
				q.Rules = append(q.Rules, RuleStatistic{RuleCode: m.RuleCode, Threshold: m.Threshold})
				idx = len(q.Rules) - 1
			}
			if m.Passed {
				q.Rules[idx].PassedCount++
			} else {
				q.Rules[idx].FailedCount++
				q.Rules[idx].FailedCatalogs = append(q.Rules[idx].FailedCatalogs, c.CatalogNumber)
			}
		}
	}
	sort.Slice(q.Rules, func(i, j int) bool { return q.Rules[i].RuleCode < q.Rules[j].RuleCode })
	sort.Slice(q.Conclusions, func(i, j int) bool { return q.Conclusions[i].CatalogNumber < q.Conclusions[j].CatalogNumber })
	return q, nil
}

func (s *Service) IssueQueue(ctx context.Context, batchID, status, severity, rule, after string, limit int) (IssueQueue, error) {
	b, e := s.GetBatch(ctx, batchID)
	if e != nil {
		return IssueQueue{}, e
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	active := map[int]domain.PlateScan{}
	for _, x := range b.ActiveScans() {
		active[x.CatalogNumber] = x
	}
	out := IssueQueue{}
	for _, i := range b.Issues {
		if status != "" && i.Status != status || severity != "" && i.Severity != severity || rule != "" && i.RuleCode != rule {
			continue
		}
		var c int
		for _, sc := range b.Scans {
			if sc.ID == i.ScanID {
				c = sc.CatalogNumber
			}
		}
		sc := active[c]
		canResolve := b.State == domain.StateRemediation && i.Status != "closed"
		kinds := []string{}
		if canResolve {
			kinds = []string{"accepted", "rescanned"}
		}
		out.Items = append(out.Items, IssueQueueItem{Issue: i, CatalogNumber: c, ActiveScanID: sc.ID, ActiveVersion: sc.Version, CanResolve: canResolve, AvailableResolutionKinds: kinds})
	}
	sort.Slice(out.Items, func(i, j int) bool {
		if out.Items[i].CatalogNumber == out.Items[j].CatalogNumber {
			return out.Items[i].Issue.ID < out.Items[j].Issue.ID
		}
		return out.Items[i].CatalogNumber < out.Items[j].CatalogNumber
	})
	if after != "" {
		start := -1
		for i := range out.Items {
			if out.Items[i].Issue.ID == after {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return IssueQueue{}, domain.NewError(domain.CodeValidation, "after 游标无效")
		}
		out.Items = out.Items[start:]
	}
	for _, i := range b.Issues {
		if i.Status == "open" {
			out.OpenCount++
		} else {
			out.ClosedCount++
		}
	}
	out.AllClosed = out.OpenCount == 0
	if len(out.Items) > limit {
		out.NextAfter = out.Items[limit-1].Issue.ID
		out.Items = out.Items[:limit]
	}
	return out, nil
}

func (s *Service) PeerReviewHistory(ctx context.Context, batchID, reviewer string) (PeerReviewHistory, error) {
	b, e := s.GetBatch(ctx, batchID)
	if e != nil {
		return PeerReviewHistory{}, e
	}
	out := PeerReviewHistory{DutySeparationPassed: true}
	if reviewer != "" {
		if reviewer == b.CreatedBy {
			out.DutySeparationPassed = false
		}
		for _, scan := range b.Scans {
			if scan.CapturedBy == reviewer {
				out.DutySeparationPassed = false
			}
		}
		if !out.DutySeparationPassed {
			out.DutySeparationDetail = "复核员不能是批次创建人或扫描采集人"
		}
	}
	for i, r := range b.PeerReviews {
		match := fmt.Sprint(r.SampleCatalogs) == fmt.Sprint(b.SampleCatalogs())
		current := map[int]domain.PlateScan{}
		for _, scan := range b.ActiveScans() {
			current[scan.CatalogNumber] = scan
		}
		for _, catalog := range r.SampleCatalogs {
			historical := domain.PlateScan{}
			for _, scan := range b.Scans {
				if scan.CatalogNumber == catalog && !scan.CapturedAt.After(r.ReviewedAt) && scan.Version > historical.Version {
					historical = scan
				}
			}
			if historical.ID == "" || current[catalog].ID != historical.ID {
				match = false
			}
		}
		out.Rounds = append(out.Rounds, PeerReviewRound{i + 1, r, match, i == len(b.PeerReviews)-1})
	}
	out.LatestRound = len(out.Rounds)
	if out.LatestRound > 0 {
		out.LatestPassed = out.Rounds[len(out.Rounds)-1].Review.Passed
	}
	out.RequiresRemediation = b.State == domain.StateRemediation
	if b.State == domain.StatePeerReview {
		out.AllowedNextAction = "submit_peer_review"
	} else if b.State == domain.StateRemediation {
		out.AllowedNextAction = "resolve_issues"
	} else {
		out.AllowedNextAction = "read_only"
	}
	return out, nil
}

func (s *Service) ManifestPreview(ctx context.Context, batchID string, expected int64) (ManifestPreview, error) {
	unlock := s.lock(batchID)
	defer unlock()
	b, e := s.GetBatch(ctx, batchID)
	if e != nil {
		return ManifestPreview{}, e
	}
	if expected > 0 && expected != b.Revision {
		return ManifestPreview{}, domain.RevisionConflict(b.Revision)
	}
	all, e := s.store.AllAudit(ctx, batchID)
	if e != nil {
		return ManifestPreview{}, e
	}
	head := audit.Head(all)
	if b.State != domain.StatePendingArchive && b.State != domain.StateSealed {
		return ManifestPreview{}, domain.NewError(domain.CodeInvalidState, "只有待封存或已封存批次可预览清单")
	}
	if b.State != domain.StateSealed && len(domain.MissingCatalogs(b)) > 0 {
		return ManifestPreview{}, domain.NewError(domain.CodeValidation, "批次仍有缺失目录")
	}
	if b.State != domain.StateSealed && domain.OpenIssueCount(b) > 0 {
		return ManifestPreview{}, domain.NewError(domain.CodeValidation, "批次仍有未关闭质量问题")
	}
	if b.State == domain.StateSealed {
		stored, err := s.store.LoadManifest(ctx, batchID)
		if err != nil {
			return ManifestPreview{}, err
		}
		return ManifestPreview{Manifest: stored, BatchRevision: b.Revision, ExpectedRevision: expected, AuditHeadHash: stored.AuditHeadHash, ManifestHash: stored.ManifestHash, ReadOnly: true}, nil
	}
	m := domain.ArchiveManifest{BatchID: b.ID, BatchRevision: b.Revision + 1, AuditHeadHash: head, Entries: []domain.ManifestEntry{}}
	for _, x := range b.ActiveScans() {
		m.Entries = append(m.Entries, domain.ManifestEntry{CatalogNumber: x.CatalogNumber, ScanID: x.ID, Version: x.Version, ContentChecksum: x.ContentChecksum, PixelWidth: x.PixelWidth, PixelHeight: x.PixelHeight, BitDepth: x.BitDepth, SupersedesScanID: x.SupersedesScanID})
	}
	h, _ := domain.HashManifest(m)
	m.ManifestHash = h
	return ManifestPreview{Manifest: m, BatchRevision: b.Revision, ExpectedRevision: expected, AuditHeadHash: head, ManifestHash: h, ReadOnly: b.State == domain.StateSealed}, nil
}

