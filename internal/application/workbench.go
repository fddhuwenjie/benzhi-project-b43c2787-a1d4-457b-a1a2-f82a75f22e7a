package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

type workbenchCursor struct {
	UpdatedAt string `json:"updated_at"`
	BatchID   string `json:"batch_id"`
	Filter    string `json:"filter"`
	Signature string `json:"signature"`
}

func workbenchFilterHash(q BatchWorkbenchQuery) string {
	raw, _ := json.Marshal([]string{q.State, q.ScannerID, q.QualityPolicyVersion, q.CreatedBy, q.Title})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func signCursor(c workbenchCursor) string {
	sum := sha256.Sum256([]byte("astroplate-workbench-v1\x00" + c.UpdatedAt + "\x00" + c.BatchID + "\x00" + c.Filter))
	return hex.EncodeToString(sum[:])
}

func encodeWorkbenchCursor(at time.Time, id, filter string) string {
	c := workbenchCursor{UpdatedAt: at.UTC().Format(time.RFC3339Nano), BatchID: id, Filter: filter}
	c.Signature = signCursor(c)
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeWorkbenchCursor(raw, filter string) (time.Time, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", domain.NewError(domain.CodeValidation, "cursor 无效或已被篡改")
	}
	var c workbenchCursor
	if json.Unmarshal(decoded, &c) != nil || c.BatchID == "" || c.Filter != filter || c.Signature != signCursor(c) {
		return time.Time{}, "", domain.NewError(domain.CodeValidation, "cursor 无效、筛选不匹配或已被篡改")
	}
	at, err := time.Parse(time.RFC3339Nano, c.UpdatedAt)
	if err != nil {
		return time.Time{}, "", domain.NewError(domain.CodeValidation, "cursor 时间无效")
	}
	return at, c.BatchID, nil
}

func validateOptionalIdentifier(name, value string) error {
	if value == "" {
		return nil
	}
	return domain.ValidateIdentifier(name, value)
}

func (s *Service) cachedWorkbenchFilter(q BatchWorkbenchQuery) persistence.BatchListFilter {
	key := workbenchFilterHash(q)
	if filter, ok := s.workbenchFilters[key]; ok {
		return filter
	}
	filter := persistence.BatchListFilter{State: q.State, ScannerID: q.ScannerID, QualityPolicyVersion: q.QualityPolicyVersion, CreatedBy: q.CreatedBy, Title: q.Title}
	s.workbenchFilters[key] = filter
	return filter
}

func (s *Service) BatchWorkbench(ctx context.Context, q BatchWorkbenchQuery) (BatchWorkbench, error) {
	if q.State != "" && !domain.IsValidBatchState(domain.BatchState(q.State)) {
		return BatchWorkbench{}, domain.NewError(domain.CodeValidation, "未知批次状态 %s", q.State)
	}
	if err := validateOptionalIdentifier("scanner_id", q.ScannerID); err != nil {
		return BatchWorkbench{}, err
	}
	if err := validateOptionalIdentifier("quality_policy_version", q.QualityPolicyVersion); err != nil {
		return BatchWorkbench{}, err
	}
	if q.CreatedBy != "" {
		if err := domain.ValidatePrincipal("created_by", q.CreatedBy); err != nil {
			return BatchWorkbench{}, err
		}
	}
	q.Title = strings.TrimSpace(q.Title)
	if len(q.Title) > 200 {
		return BatchWorkbench{}, domain.NewError(domain.CodeValidation, "title 关键字不能超过 200 字节")
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 {
		return BatchWorkbench{}, domain.NewError(domain.CodeValidation, "limit 必须在 1 到 200 之间")
	}
	filterHash := workbenchFilterHash(q)
	var cursorAt time.Time
	var cursorID string
	var err error
	if q.Cursor != "" {
		cursorAt, cursorID, err = decodeWorkbenchCursor(q.Cursor, filterHash)
		if err != nil {
			return BatchWorkbench{}, err
		}
	}
	records, err := s.store.ListBatchRecords(ctx, s.cachedWorkbenchFilter(q))
	if err != nil {
		return BatchWorkbench{}, err
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return records[i].Batch.ID < records[j].Batch.ID
		}
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	out := BatchWorkbench{Items: []BatchWorkbenchItem{}, Total: len(records), StateCounts: []BatchStateCount{}}
	counts := map[domain.BatchState]int{}
	for _, record := range records {
		if err = s.store.ValidateBatchProjection(ctx, record.Batch); err != nil {
			return BatchWorkbench{}, err
		}
		counts[record.Batch.State]++
	}
	states := []domain.BatchState{domain.StateDraft, domain.StateCapturing, domain.StateQualityReview, domain.StateRemediation, domain.StatePeerReview, domain.StatePendingArchive, domain.StateSealed}
	for _, state := range states {
		if count := counts[state]; count > 0 {
			out.StateCounts = append(out.StateCounts, BatchStateCount{State: state, Count: count})
		}
	}
	eligible := make([]persistence.BatchListRecord, 0, len(records))
	for _, record := range records {
		if !cursorAt.IsZero() && (record.UpdatedAt.After(cursorAt) || record.UpdatedAt.Equal(cursorAt) && record.Batch.ID <= cursorID) {
			continue
		}
		eligible = append(eligible, record)
	}
	page := eligible
	if len(page) > q.Limit {
		page = page[:q.Limit]
	}
	seen := map[string]bool{}
	for _, record := range page {
		b := record.Batch
		if seen[b.ID] {
			return BatchWorkbench{}, domain.NewError(domain.CodeIntegrity, "批次列表页包含重复 batch_id")
		}
		seen[b.ID] = true
		missing, open := len(domain.MissingCatalogs(b)), domain.OpenIssueCount(b)
		out.Items = append(out.Items, BatchWorkbenchItem{BatchID: b.ID, Title: b.Title, State: b.State, Revision: b.Revision, ScannerID: b.ScannerID, QualityPolicyVersion: b.QualityPolicyVersion, CreatedBy: b.CreatedBy, CreatedAt: b.CreatedAt, UpdatedAt: record.UpdatedAt, CatalogCount: b.CatalogEnd - b.CatalogStart + 1, ActiveScanCount: len(b.ActiveScans()), MissingCatalogCount: missing, OpenIssueCount: open, NextAction: domain.NextBatchAction(b.State, missing, open)})
	}
	if len(eligible) > len(page) && len(page) > 0 {
		last := page[len(page)-1]
		out.NextCursor = encodeWorkbenchCursor(last.UpdatedAt, last.Batch.ID, filterHash)
	}
	return out, nil
}

func draftView(d *domain.PeerReviewDraft, batchRevision int64) *PeerReviewDraftView {
	missing := d.MissingCatalogs()
	valid := len(missing) == 0
	for _, evidence := range d.Evidence {
		if !evidence.ChecksumMatch || !evidence.DimensionsMatch || !evidence.BitDepthMatch {
			valid = false
		}
	}
	return &PeerReviewDraftView{Draft: d, BatchRevision: batchRevision, CompletedCount: len(d.Evidence), MissingCatalogs: missing, AllEvidenceValid: valid}
}

func (s *Service) ResolveByRescan(ctx context.Context, batchID string, r RescanResolutionRequest) (CommandResult, error) {
	if err := validateMeta(r.CommandMeta, false); err != nil {
		return CommandResult{}, err
	}
	if err := domain.ValidateIdentifier("target_scan_id", r.TargetScanID); err != nil {
		return CommandResult{}, err
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
			result, e = replayOrConflict(record, "rescan_resolution", hash)
			return e
		}
		batch, e := tx.LoadBatch(ctx, batchID)
		if e != nil {
			return e
		}
		if batch.Revision != r.ExpectedRevision {
			return domain.RevisionConflict(batch.Revision)
		}
		stored := batch.Revision
		replacement := domain.PlateScan{ID: newID("scan"), ContentChecksum: r.ContentChecksum, PixelWidth: r.PixelWidth, PixelHeight: r.PixelHeight, BitDepth: r.BitDepth, ExposureScore: r.ExposureScore, FocusScore: r.FocusScore}
		resolution, e := batch.ResolveIssuesByRescan(r.TargetScanID, replacement, r.Actor, s.now())
		if e != nil {
			return e
		}
		if e = tx.SaveBatch(ctx, batch, stored); e != nil {
			return e
		}
		if e = appendEvent(ctx, tx, batch, "issues.rescan_resolved", r.Actor, map[string]any{"request_id": r.RequestID, "lineage": map[string]string{"superseded_scan_id": resolution.OldScanID, "replacement_scan_id": resolution.NewScanID}, "rule_results": resolution.RuleResults, "closed_issue_ids": resolution.ClosedIssueIDs}, s.now()); e != nil {
			return e
		}
		result = CommandResult{Batch: batch, RescanResolution: &resolution, ClosedCount: len(resolution.ClosedIssueIDs), RemainingOpenCount: resolution.RemainingOpen, CanRequestPeerReview: resolution.CanPeerReview}
		return putResult(ctx, tx, r.RequestID, batchID, "rescan_resolution", hash, result)
	})
	return result, err
}

func sortedEvidenceCatalogs(d *domain.PeerReviewDraft) []int {
	values := make([]int, 0, len(d.Evidence))
	for _, item := range d.Evidence {
		values = append(values, item.CatalogNumber)
	}
	sort.Ints(values)
	return values
}
