package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"astroplate-vault/internal/domain"
)

func preparePeerReviewBatch(t *testing.T, s *Service, id string, count int) *domain.PlateBatch {
	t.Helper()
	ctx := context.Background()
	b := createCapturingBatch(t, s, id, 1, count)
	items := make([]BatchScanItem, 0, count)
	for catalog := 1; catalog <= count; catalog++ {
		items = append(items, BatchScanItem{CatalogNumber: catalog, ContentChecksum: fmt.Sprintf("checksum-%s-%d", id, catalog), PixelWidth: 8000, PixelHeight: 8000, BitDepth: 16, ExposureScore: .9, FocusScore: .95})
	}
	added, err := s.BatchAddScans(ctx, id, BatchScanRequest{CommandMeta: CommandMeta{RequestID: "all-scans-" + id, ExpectedRevision: b.Revision, Actor: "operator"}, Scans: items})
	if err != nil {
		t.Fatal(err)
	}
	evaluated, err := s.Evaluate(ctx, id, EvaluateRequest{CommandMeta: CommandMeta{RequestID: "evaluate-" + id, ExpectedRevision: added.Batch.Revision, Actor: "operator"}})
	if err != nil {
		t.Fatal(err)
	}
	requested, err := s.RequestPeerReview(ctx, id, PeerReviewRequestRequest{CommandMeta: CommandMeta{RequestID: "peer-request-" + id, ExpectedRevision: evaluated.Batch.Revision, Actor: "operator"}})
	if err != nil {
		t.Fatal(err)
	}
	return requested.Batch
}

func TestWorkbenchFiltersCursorAndMetrics(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("workbench-%d", i)
		_, err := s.CreateBatch(ctx, CreateBatchRequest{CommandMeta: CommandMeta{RequestID: "create-" + id, Actor: "creator"}, ID: id, Title: fmt.Sprintf("猎户座 %d", i), CatalogStart: 1, CatalogEnd: 2, ScannerID: "scanner-a", QualityPolicyVersion: "v1"})
		if err != nil {
			t.Fatal(err)
		}
	}
	page1, err := s.BatchWorkbench(ctx, BatchWorkbenchQuery{ScannerID: "scanner-a", Title: "猎户座", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page1.Total != 4 || len(page1.Items) != 2 || page1.NextCursor == "" || page1.Items[0].MissingCatalogCount != 2 || page1.Items[0].NextAction != "submit_calibration" {
		t.Fatalf("工作台首屏错误: %#v", page1)
	}
	page2, err := s.BatchWorkbench(ctx, BatchWorkbenchQuery{ScannerID: "scanner-a", Title: "猎户座", Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range append(page1.Items, page2.Items...) {
		if seen[item.BatchID] {
			t.Fatalf("游标翻页重复批次 %s", item.BatchID)
		}
		seen[item.BatchID] = true
	}
	if len(seen) != 4 {
		t.Fatalf("游标未遍历完整集合: %#v", seen)
	}
	if _, err = s.BatchWorkbench(ctx, BatchWorkbenchQuery{State: "unknown", Limit: 2}); err == nil {
		t.Fatal("未知状态未被拒绝")
	}
	if _, err = s.BatchWorkbench(ctx, BatchWorkbenchQuery{ScannerID: "scanner-a", Title: "猎户座", Limit: 2, Cursor: page1.NextCursor + "x"}); err == nil {
		t.Fatal("篡改游标未被拒绝")
	}
}

func TestAtomicRescanResolutionRollbackAndReplay(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	b := createCapturingBatch(t, s, "rescan-atomic", 1, 1)
	bad, err := s.AddScan(ctx, b.ID, ScanRequest{CommandMeta: CommandMeta{RequestID: "bad-two-rules", ExpectedRevision: b.Revision, Actor: "operator"}, CatalogNumber: 1, ContentChecksum: "bad-two-rules-checksum", PixelWidth: 8000, PixelHeight: 8000, BitDepth: 16, ExposureScore: .2, FocusScore: .2})
	if err != nil {
		t.Fatal(err)
	}
	evaluated, err := s.Evaluate(ctx, b.ID, EvaluateRequest{CommandMeta: CommandMeta{RequestID: "evaluate-two-rules", ExpectedRevision: bad.Batch.Revision, Actor: "operator"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(evaluated.Batch.Issues) != 2 {
		t.Fatalf("预期两个质量问题，得到 %d", len(evaluated.Batch.Issues))
	}
	failed := RescanResolutionRequest{CommandMeta: CommandMeta{RequestID: "rescan-still-bad", ExpectedRevision: evaluated.Batch.Revision, Actor: "operator"}, TargetScanID: evaluated.Batch.ActiveScans()[0].ID, ContentChecksum: "replacement-still-bad", PixelWidth: 8000, PixelHeight: 8000, BitDepth: 16, ExposureScore: .9, FocusScore: .2}
	if _, err = s.ResolveByRescan(ctx, b.ID, failed); err == nil {
		t.Fatal("仍未通过聚焦规则的重扫未回滚")
	}
	afterFailure, err := s.GetBatch(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure.Revision != evaluated.Batch.Revision || len(afterFailure.Scans) != 1 || domain.OpenIssueCount(afterFailure) != 2 {
		t.Fatalf("失败重扫污染聚合: %#v", afterFailure)
	}
	if record, err := s.store.GetIdempotency(ctx, failed.RequestID); err != nil || record != nil {
		t.Fatalf("失败重扫写入幂等记录: %#v %v", record, err)
	}
	success := failed
	success.RequestID = "rescan-passing"
	success.ContentChecksum = "replacement-passing"
	success.FocusScore = .95
	resolved, err := s.ResolveByRescan(ctx, b.ID, success)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Batch.Revision != evaluated.Batch.Revision+1 || len(resolved.Batch.Scans) != 2 || resolved.ClosedCount != 2 || !resolved.CanRequestPeerReview {
		t.Fatalf("原子重扫结果错误: %#v", resolved)
	}
	replayed, err := s.ResolveByRescan(ctx, b.ID, success)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.RescanResolution.NewScanID != resolved.RescanResolution.NewScanID {
		t.Fatalf("重扫幂等重放错误: %#v", replayed)
	}
	changed := success
	changed.ContentChecksum = "replacement-different"
	_, err = s.ResolveByRescan(ctx, b.ID, changed)
	var business *domain.Error
	if !errors.As(err, &business) || business.Code != domain.CodeIdempotency {
		t.Fatalf("异载荷未返回幂等冲突: %v", err)
	}
}

func TestPeerReviewDraftPersistsAndCompletes(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	b := preparePeerReviewBatch(t, s, "draft-persist", 21)
	created, err := s.CreatePeerReviewDraft(ctx, b.ID, CreatePeerReviewDraftRequest{CommandMeta: CommandMeta{RequestID: "create-draft", ExpectedRevision: b.Revision, Actor: "reviewer"}})
	if err != nil {
		t.Fatal(err)
	}
	draft := created.PeerReviewDraft.Draft
	if len(draft.Samples) != 5 {
		t.Fatalf("固定样本应为五个: %#v", draft.Samples)
	}
	for i, sample := range draft.Samples[:3] {
		request := PutPeerReviewDraftEvidenceRequest{CommandMeta: CommandMeta{RequestID: fmt.Sprintf("draft-item-%d", i), ExpectedRevision: b.Revision, Actor: "reviewer"}, ExpectedDraftRevision: draft.DraftRevision, CatalogNumber: sample.CatalogNumber, ScanID: sample.ScanID, Version: sample.Version, ObservedChecksum: sample.ContentChecksum, DimensionsMatch: true, BitDepthMatch: true}
		result, err := s.PutPeerReviewDraftEvidence(ctx, b.ID, draft.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		draft = result.PeerReviewDraft.Draft
	}
	restarted := NewService(s.store)
	restored, err := restarted.GetPeerReviewDraft(ctx, b.ID, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.CompletedCount != 3 || len(restored.MissingCatalogs) != 2 {
		t.Fatalf("重启恢复的草稿进度错误: %#v", restored)
	}
	incomplete := CompletePeerReviewDraftRequest{CommandMeta: CommandMeta{RequestID: "complete-too-early", ExpectedRevision: b.Revision, Actor: "reviewer"}, ExpectedDraftRevision: draft.DraftRevision}
	if _, err = restarted.CompletePeerReviewDraft(ctx, b.ID, draft.ID, incomplete); err == nil {
		t.Fatal("缺少样本时完成草稿未被拒绝")
	}
	for i, sample := range draft.Samples[3:] {
		request := PutPeerReviewDraftEvidenceRequest{CommandMeta: CommandMeta{RequestID: fmt.Sprintf("draft-rest-%d", i), ExpectedRevision: b.Revision, Actor: "reviewer"}, ExpectedDraftRevision: draft.DraftRevision, CatalogNumber: sample.CatalogNumber, ScanID: sample.ScanID, Version: sample.Version, ObservedChecksum: sample.ContentChecksum, DimensionsMatch: true, BitDepthMatch: true}
		result, err := restarted.PutPeerReviewDraftEvidence(ctx, b.ID, draft.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		draft = result.PeerReviewDraft.Draft
	}
	complete := CompletePeerReviewDraftRequest{CommandMeta: CommandMeta{RequestID: "complete-draft", ExpectedRevision: b.Revision, Actor: "reviewer"}, ExpectedDraftRevision: draft.DraftRevision}
	finished, err := restarted.CompletePeerReviewDraft(ctx, b.ID, draft.ID, complete)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Batch.State != domain.StatePendingArchive || finished.Batch.Revision != b.Revision+1 || finished.PeerReviewDraft.Draft.Status != domain.PeerReviewDraftCompleted {
		t.Fatalf("草稿完成结果错误: %#v", finished)
	}
	replayed, err := restarted.CompletePeerReviewDraft(ctx, b.ID, draft.ID, complete)
	if err != nil || !replayed.Replayed || len(replayed.Batch.PeerReviews) != 1 {
		t.Fatalf("草稿完成幂等重放错误: %#v %v", replayed, err)
	}
}

func TestPeerReviewDraftDerivesFailuresPerSample(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	b := preparePeerReviewBatch(t, s, "draft-failures", 6)
	created, err := s.CreatePeerReviewDraft(ctx, b.ID, CreatePeerReviewDraftRequest{CommandMeta: CommandMeta{RequestID: "create-failure-draft", ExpectedRevision: b.Revision, Actor: "reviewer"}})
	if err != nil {
		t.Fatal(err)
	}
	draft := created.PeerReviewDraft.Draft
	if len(draft.Samples) != 2 {
		t.Fatalf("测试批次应产生两个固定样本: %#v", draft.Samples)
	}
	wrong := PutPeerReviewDraftEvidenceRequest{CommandMeta: CommandMeta{RequestID: "wrong-frozen-scan", ExpectedRevision: b.Revision, Actor: "reviewer"}, ExpectedDraftRevision: draft.DraftRevision, CatalogNumber: draft.Samples[0].CatalogNumber, ScanID: draft.Samples[1].ScanID, Version: draft.Samples[0].Version, ObservedChecksum: draft.Samples[0].ContentChecksum, DimensionsMatch: true, BitDepthMatch: true}
	if _, err = s.PutPeerReviewDraftEvidence(ctx, b.ID, draft.ID, wrong); err == nil {
		t.Fatal("错误冻结扫描身份未被拒绝")
	}
	for i, sample := range draft.Samples {
		request := PutPeerReviewDraftEvidenceRequest{CommandMeta: CommandMeta{RequestID: fmt.Sprintf("failed-evidence-%d", i), ExpectedRevision: b.Revision, Actor: "reviewer"}, ExpectedDraftRevision: draft.DraftRevision, CatalogNumber: sample.CatalogNumber, ScanID: sample.ScanID, Version: sample.Version, ObservedChecksum: fmt.Sprintf("mismatch-checksum-%d", i), DimensionsMatch: true, BitDepthMatch: true}
		result, err := s.PutPeerReviewDraftEvidence(ctx, b.ID, draft.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		draft = result.PeerReviewDraft.Draft
	}
	request := CompletePeerReviewDraftRequest{CommandMeta: CommandMeta{RequestID: "complete-failure-draft", ExpectedRevision: b.Revision, Actor: "reviewer"}, ExpectedDraftRevision: draft.DraftRevision}
	completed, err := s.CompletePeerReviewDraft(ctx, b.ID, draft.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Batch.State != domain.StateRemediation || domain.OpenIssueCount(completed.Batch) != 2 {
		t.Fatalf("失败证据未按样本生成整改问题: %#v", completed.Batch.Issues)
	}
	replayed, err := s.CompletePeerReviewDraft(ctx, b.ID, draft.ID, request)
	if err != nil || !replayed.Replayed || domain.OpenIssueCount(replayed.Batch) != 2 || len(replayed.Batch.PeerReviews) != 1 {
		t.Fatalf("失败完成重放重复生成业务记录: %#v %v", replayed, err)
	}
}
