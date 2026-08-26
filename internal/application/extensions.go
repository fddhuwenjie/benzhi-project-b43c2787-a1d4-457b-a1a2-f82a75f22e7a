package application

import (
	"context"
	"encoding/json"
	"sort"

	"astroplate-vault/internal/audit"
	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

func (s *Service) ReviseBatch(ctx context.Context, batchID string, r ReviseBatchRequest) (CommandResult, error) {
	return s.mutate(ctx, batchID, "revise_batch", "batch.metadata_revised", r.CommandMeta, r, func(b *domain.PlateBatch) (any, error) {
		title, start, end, scanner, policy := b.Title, b.CatalogStart, b.CatalogEnd, b.ScannerID, b.QualityPolicyVersion
		if r.Title != nil {
			title = *r.Title
		}
		if r.CatalogStart != nil {
			start = *r.CatalogStart
		}
		if r.CatalogEnd != nil {
			end = *r.CatalogEnd
		}
		if r.ScannerID != nil {
			scanner = *r.ScannerID
		}
		if r.QualityPolicyVersion != nil {
			policy = *r.QualityPolicyVersion
		}
		changes, err := b.Revise(title, start, end, scanner, policy)
		if err != nil {
			return nil, err
		}
		return map[string]any{"changes": changes}, nil
	})
}

func (s *Service) ResolveIssuesBatch(ctx context.Context, batchID string, r BatchIssueResolutionRequest) (CommandResult, error) {
	if err := validateMeta(r.CommandMeta, false); err != nil {
		return CommandResult{}, err
	}
	if len(r.Resolutions) < 1 || len(r.Resolutions) > 100 {
		return CommandResult{}, domain.NewError(domain.CodeValidation, "resolutions 数量必须在 1 到 100 之间")
	}
	hash, err := payloadHash(r)
	if err != nil {
		return CommandResult{}, err
	}
	unlock := s.lock(batchID)
	defer unlock()
	var result CommandResult
	err = s.store.WithTx(ctx, func(tx *persistence.Tx) error {
		record, e := tx.GetIdempotency(ctx, r.RequestID)
		if e != nil {
			return e
		}
		if record != nil {
			result, e = s.replayOrConflict(record, "resolve_issues_batch", hash)
			return e
		}
		b, e := tx.LoadBatch(ctx, batchID)
		if e != nil {
			return e
		}
		if b.Revision != r.ExpectedRevision {
			return domain.RevisionConflict(b.Revision)
		}
		stored := b.Revision
		items := make([]domain.IssueResolution, 0, len(r.Resolutions))
		for _, x := range r.Resolutions {
			items = append(items, domain.IssueResolution{IssueID: x.IssueID, ResolutionKind: x.ResolutionKind, ResolutionNote: x.ResolutionNote, ReplacementScanID: x.ReplacementScanID})
		}
		if e = b.ResolveIssues(items, r.Actor, s.now()); e != nil {
			return e
		}
		sort.Slice(items, func(i, j int) bool { return items[i].IssueID < items[j].IssueID })
		open := domain.OpenIssueCount(b)
		if e = tx.SaveBatch(ctx, b, stored); e != nil {
			return e
		}
		if e = appendEvent(ctx, tx, b, "issues.resolved_batch", r.Actor, map[string]any{"request_id": r.RequestID, "resolutions": items, "resolved_by": r.Actor}, s.now()); e != nil {
			return e
		}
		result = CommandResult{Batch: b, ClosedCount: len(items), RemainingOpenCount: open, CanRequestPeerReview: open == 0}
		return putResult(ctx, tx, r.RequestID, batchID, "resolve_issues_batch", hash, result)
	})
	return result, err
}

func (s *Service) SubmitPeerReviewEvidence(ctx context.Context, batchID string, r PeerReviewEvidenceRequest) (CommandResult, error) {
	return s.mutate(ctx, batchID, "submit_peer_review_evidence", "peer_review.evidence_completed", r.CommandMeta, r, func(b *domain.PlateBatch) (any, error) {
		inputs := make([]domain.PeerEvidenceInput, 0, len(r.Evidence))
		for _, x := range r.Evidence {
			inputs = append(inputs, domain.PeerEvidenceInput{CatalogNumber: x.CatalogNumber, ObservedChecksum: x.ObservedChecksum, DimensionsMatch: x.DimensionsMatch, BitDepthMatch: x.BitDepthMatch, Note: x.Note})
		}
		if err := b.RecordPeerReviewEvidence(r.Actor, inputs, func() string { return newID("issue") }, s.now()); err != nil {
			return nil, err
		}
		return b.PeerReviews[len(b.PeerReviews)-1], nil
	})
}

func metricGates(c domain.CalibrationSession) (MetricGate, MetricGate, MetricGate) {
	return MetricGate{float64(c.ResolutionDPI), ">=2400", c.ResolutionDPI >= domain.MinResolutionDPI}, MetricGate{c.GrayResponseError, "<=0.05", c.GrayResponseError <= domain.MaxGrayResponseError}, MetricGate{c.GeometryErrorPercent, "<=0.20", c.GeometryErrorPercent <= domain.MaxGeometryErrorPercent}
}

func (s *Service) CalibrationHistory(ctx context.Context, batchID, resultFilter string) (CalibrationDetail, error) {
	if resultFilter != "" && resultFilter != "passed" && resultFilter != "failed" {
		return CalibrationDetail{}, domain.NewError(domain.CodeValidation, "result 必须为 passed 或 failed")
	}
	b, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return CalibrationDetail{}, err
	}
	if len(b.Calibrations) == 0 {
		return CalibrationDetail{}, domain.NewError(domain.CodeNotCalibrated, "批次尚未标定")
	}
	all := append([]domain.CalibrationSession(nil), b.Calibrations...)
	sort.Slice(all, func(i, j int) bool {
		if all[i].PerformedAt.Equal(all[j].PerformedAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].PerformedAt.Before(all[j].PerformedAt)
	})
	current := all[len(all)-1]
	r, g, geo := metricGates(current)
	out := CalibrationDetail{BatchID: batchID, Calibration: &current, CurrentSession: &current, BatchState: b.State, GateResult: current.Result, Resolution: r, GrayResponse: g, Geometry: geo, FailureReasons: domain.CalibrationFailureReasons(current), AttemptCount: len(all), Sessions: []CalibrationAttempt{}}
	for _, c := range all {
		if resultFilter != "" && c.Result != resultFilter {
			continue
		}
		cr, cg, cgeo := metricGates(c)
		out.Sessions = append(out.Sessions, CalibrationAttempt{Session: c, Resolution: cr, GrayResponse: cg, Geometry: cgeo, FailureReasons: domain.CalibrationFailureReasons(c)})
	}
	return out, nil
}

type ScanProgressQuery struct {
	CatalogStart, CatalogEnd int
	Status                   string
	AfterCatalog, Limit      int
}

func (s *Service) ScanProgress(ctx context.Context, batchID string, q ScanProgressQuery) (ScanProgress, error) {
	b, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return ScanProgress{}, err
	}
	if q.CatalogStart == 0 {
		q.CatalogStart = b.CatalogStart
	}
	if q.CatalogEnd == 0 {
		q.CatalogEnd = b.CatalogEnd
	}
	if q.CatalogStart < b.CatalogStart || q.CatalogEnd > b.CatalogEnd || q.CatalogStart > q.CatalogEnd {
		return ScanProgress{}, domain.NewError(domain.CodeValidation, "目录筛选区间必须位于批次边界内")
	}
	if q.Status != "" && q.Status != "missing" && q.Status != "captured" && q.Status != "superseded" {
		return ScanProgress{}, domain.NewError(domain.CodeValidation, "status 必须为 missing、captured 或 superseded")
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 1 || q.Limit > 500 {
		return ScanProgress{}, domain.NewError(domain.CodeValidation, "limit 必须在 1 到 500 之间")
	}
	if q.AfterCatalog != 0 && (q.AfterCatalog < q.CatalogStart-1 || q.AfterCatalog >= q.CatalogEnd) {
		return ScanProgress{}, domain.NewError(domain.CodeValidation, "after_catalog 超出查询区间")
	}
	if err = domain.ValidateScanLineage(b); err != nil {
		return ScanProgress{}, err
	}
	versions := map[int][]domain.PlateScan{}
	for _, scan := range b.Scans {
		versions[scan.CatalogNumber] = append(versions[scan.CatalogNumber], scan)
	}
	out := ScanProgress{BatchID: b.ID, BatchRevision: b.Revision, ExpectedCount: b.CatalogEnd - b.CatalogStart + 1, Items: []CatalogProgress{}}
	for c := b.CatalogStart; c <= b.CatalogEnd; c++ {
		if len(versions[c]) == 0 {
			out.MissingCount++
		} else {
			out.CapturedCount++
			out.ReplacementCount += len(versions[c]) - 1
		}
	}
	out.CompletionPercent = float64(out.CapturedCount) * 100 / float64(out.ExpectedCount)
	for c := q.CatalogStart; c <= q.CatalogEnd; c++ {
		if c <= q.AfterCatalog {
			continue
		}
		scans := versions[c]
		sort.Slice(scans, func(i, j int) bool { return scans[i].Version < scans[j].Version })
		status := "missing"
		if len(scans) > 0 {
			status = "captured"
		}
		matches := q.Status == "" || q.Status == status || q.Status == "superseded" && len(scans) > 1
		if !matches {
			continue
		}
		item := CatalogProgress{CatalogNumber: c, Status: status, Scans: []ScanVersionProgress{}}
		for i, scan := range scans {
			st := "superseded"
			if i == len(scans)-1 {
				st = "captured"
				item.ActiveScanID = scan.ID
			}
			item.Scans = append(item.Scans, ScanVersionProgress{PlateScan: scan, Status: st})
		}
		out.Items = append(out.Items, item)
		if len(out.Items) == q.Limit {
			for next := c + 1; next <= q.CatalogEnd; next++ {
				if q.Status == "" || q.Status == "missing" && len(versions[next]) == 0 || q.Status == "captured" && len(versions[next]) > 0 || q.Status == "superseded" && len(versions[next]) > 1 {
					out.NextAfterCatalog = c
					break
				}
			}
			break
		}
	}
	return out, nil
}

func (s *Service) ArchiveReadiness(ctx context.Context, batchID string, expected int64) (ArchiveReadiness, error) {
	if expected < 1 {
		return ArchiveReadiness{}, domain.NewError(domain.CodeValidation, "expected_revision 必须为正整数")
	}
	b, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return ArchiveReadiness{}, err
	}
	if b.Revision != expected {
		return ArchiveReadiness{}, domain.RevisionConflict(b.Revision)
	}
	events, err := s.store.AllAudit(ctx, batchID)
	if err != nil {
		return ArchiveReadiness{}, err
	}
	if err = audit.Verify(events); err != nil {
		return ArchiveReadiness{}, err
	}
	out := ArchiveReadiness{BatchID: batchID, ExpectedRevision: expected, CurrentRevision: b.Revision, AuditHeadHash: audit.Head(events), Blockers: []ReadinessBlocker{}}
	for _, code := range domain.EnsureArchiveBusinessReady(b) {
		out.Blockers = append(out.Blockers, ReadinessBlocker{Code: code, Detail: domain.DescribeBlocker(code, b)})
	}
	out.CanSeal = len(out.Blockers) == 0
	if out.CanSeal {
		m := domain.ArchiveManifest{BatchID: b.ID, BatchRevision: b.Revision + 1, AuditHeadHash: out.AuditHeadHash, Entries: []domain.ManifestEntry{}}
		for _, x := range b.ActiveScans() {
			m.Entries = append(m.Entries, domain.ManifestEntry{CatalogNumber: x.CatalogNumber, ScanID: x.ID, Version: x.Version, ContentChecksum: x.ContentChecksum, PixelWidth: x.PixelWidth, PixelHeight: x.PixelHeight, BitDepth: x.BitDepth, SupersedesScanID: x.SupersedesScanID})
		}
		out.ManifestHash, _ = domain.HashManifest(m)
	}
	return out, nil
}

type AuditFilter struct {
	EventType, Actor, RequestID             string
	RevisionFrom, RevisionTo, After, Before int64
	Limit                                   int
}

func (s *Service) SearchAudit(ctx context.Context, batchID string, q AuditFilter) (AuditPage, error) {
	if _, err := s.store.LoadBatch(ctx, batchID); err != nil {
		return AuditPage{}, err
	}
	if q.RequestID != "" {
		if err := domain.ValidateIdentifier("request_id", q.RequestID); err != nil {
			return AuditPage{}, err
		}
	}
	if q.Actor != "" {
		if err := domain.ValidatePrincipal("actor", q.Actor); err != nil {
			return AuditPage{}, err
		}
	}
	if q.RevisionFrom < 0 || q.RevisionTo < 0 || q.RevisionTo > 0 && q.RevisionFrom > q.RevisionTo {
		return AuditPage{}, domain.NewError(domain.CodeValidation, "revision_from 与 revision_to 区间无效")
	}
	if q.After < 0 || q.Before < 0 || q.Before > 0 && q.Before <= q.After {
		return AuditPage{}, domain.NewError(domain.CodeValidation, "after 与 before 区间无效")
	}
	if q.Limit < 1 || q.Limit > 200 {
		return AuditPage{}, domain.NewError(domain.CodeValidation, "limit 必须在 1 到 200 之间")
	}
	all, err := s.store.AllAudit(ctx, batchID)
	if err != nil {
		return AuditPage{}, err
	}
	if err = audit.Verify(all); err != nil {
		return AuditPage{HeadHash: audit.Head(all), Verified: false, FirstInvalidSequence: firstInvalid(all), IntegrityDetail: err.Error()}, err
	}
	matched := []domain.AuditEvent{}
	for _, e := range all {
		if e.Sequence <= q.After || q.Before > 0 && e.Sequence >= q.Before || q.EventType != "" && e.EventType != q.EventType || q.Actor != "" && e.Actor != q.Actor || q.RevisionFrom > 0 && e.Revision < q.RevisionFrom || q.RevisionTo > 0 && e.Revision > q.RevisionTo || q.RequestID != "" && !payloadHasRequestID(e.Payload, q.RequestID) {
			continue
		}
		matched = append(matched, e)
	}
	out := AuditPage{Events: []domain.AuditEvent{}, HeadHash: audit.Head(all), Verified: true, MatchedCount: len(matched), SummaryByEventType: auditGroups(matched, func(e domain.AuditEvent) string { return e.EventType }), SummaryByActor: auditGroups(matched, func(e domain.AuditEvent) string { return e.Actor })}
	for i := range out.SummaryByEventType {
		out.SummaryByEventType[i].EventType = out.SummaryByEventType[i].Key
	}
	for i := range out.SummaryByActor {
		out.SummaryByActor[i].Actor = out.SummaryByActor[i].Key
	}
	out.Summary = AuditSummary{MatchedCount: len(matched), ByEventType: out.SummaryByEventType, ByActor: out.SummaryByActor}
	if len(matched) > q.Limit {
		matched = matched[:q.Limit]
	}
	out.Events = matched
	out.NextAfter = q.After
	if len(matched) > 0 {
		out.NextAfter = matched[len(matched)-1].Sequence
	}
	return out, nil
}

func payloadHasRequestID(raw json.RawMessage, want string) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	var walk func(any) bool
	walk = func(x any) bool {
		switch n := x.(type) {
		case map[string]any:
			for k, v := range n {
				if k == "request_id" && v == want {
					return true
				}
				if walk(v) {
					return true
				}
			}
		case []any:
			for _, v := range n {
				if walk(v) {
					return true
				}
			}
		}
		return false
	}
	return walk(v)
}
func auditGroups(events []domain.AuditEvent, key func(domain.AuditEvent) string) []AuditGroup {
	m := map[string]*AuditGroup{}
	for _, e := range events {
		k := key(e)
		g := m[k]
		if g == nil {
			g = &AuditGroup{Key: k, FirstSequence: e.Sequence, FirstRevision: e.Revision}
			m[k] = g
		}
		g.Count++
		g.LastSequence = e.Sequence
		g.LastRevision = e.Revision
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]AuditGroup, 0, len(keys))
	for _, k := range keys {
		out = append(out, *m[k])
	}
	return out
}
