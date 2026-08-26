package application

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	store, err := persistence.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return NewService(store)
}

func createCapturingBatch(t *testing.T, s *Service, id string, start, end int) *domain.PlateBatch {
	t.Helper()
	ctx := context.Background()
	created, err := s.CreateBatch(ctx, CreateBatchRequest{CommandMeta: CommandMeta{RequestID: "create-" + id, Actor: "creator"}, ID: id, Title: "扩展验收", CatalogStart: start, CatalogEnd: end, ScannerID: "scanner", QualityPolicyVersion: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	calibrated, err := s.Calibrate(ctx, id, CalibrationRequest{CommandMeta: CommandMeta{RequestID: "calibrate-" + id, ExpectedRevision: created.Batch.Revision, Actor: "operator"}, ResolutionDPI: 3200, GrayResponseError: .01, GeometryErrorPercent: .05})
	if err != nil {
		t.Fatal(err)
	}
	return calibrated.Batch
}

func addPassingScan(t *testing.T, s *Service, batchID, requestID string, revision int64, catalog int, actor string) *domain.PlateBatch {
	t.Helper()
	result, err := s.AddScan(context.Background(), batchID, ScanRequest{CommandMeta: CommandMeta{RequestID: requestID, ExpectedRevision: revision, Actor: actor}, CatalogNumber: catalog, ContentChecksum: fmt.Sprintf("checksum-%s-%d", requestID, catalog), PixelWidth: 8000, PixelHeight: 8000, BitDepth: 16, ExposureScore: .9, FocusScore: .95})
	if err != nil {
		t.Fatal(err)
	}
	return result.Batch
}

func TestCorrectionAndQualityPreviewUseActiveVersion(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	b := createCapturingBatch(t, s, "correction-batch", 1, 2)
	b = addPassingScan(t, s, b.ID, "scan-original", b.Revision, 1, "operator")
	oldID := b.ActiveScans()[0].ID
	request := ScanCorrectionRequest{CommandMeta: CommandMeta{RequestID: "correct-scan", ExpectedRevision: b.Revision, Actor: "operator"}, CatalogNumber: 2, ContentChecksum: "checksum-corrected", PixelWidth: 8100, PixelHeight: 8200, BitDepth: 16, ExposureScore: .91, FocusScore: .96, Reason: "原目录绑定错误"}
	corrected, err := s.CorrectScan(ctx, b.ID, oldID, request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.CorrectScan(ctx, b.ID, oldID, request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || len(replayed.Batch.Scans) != 2 {
		t.Fatalf("更正幂等重放错误: %#v", replayed)
	}
	b = addPassingScan(t, s, b.ID, "scan-replacement-catalog", corrected.Batch.Revision, 1, "operator")
	preview, err := s.QualityPreview(ctx, b.ID, b.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.CanEvaluate || preview.CapturedCount != 2 || preview.Summary.Failed != 0 {
		t.Fatalf("质量预览错误: %#v", preview)
	}
	formal, err := s.Evaluate(ctx, b.ID, EvaluateRequest{CommandMeta: CommandMeta{RequestID: "formal-evaluate", ExpectedRevision: b.Revision, Actor: "operator"}})
	if err != nil {
		t.Fatal(err)
	}
	if formal.QualitySummary == nil || *formal.QualitySummary != preview.Summary {
		t.Fatalf("预览与正式评估不一致: %#v / %#v", preview.Summary, formal.QualitySummary)
	}
}

func TestResolutionRevocationHistoryAndIdempotency(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	b := createCapturingBatch(t, s, "revocation-batch", 1, 1)
	added, err := s.AddScan(ctx, b.ID, ScanRequest{CommandMeta: CommandMeta{RequestID: "bad-scan", ExpectedRevision: b.Revision, Actor: "operator"}, CatalogNumber: 1, ContentChecksum: "checksum-bad-focus", PixelWidth: 8000, PixelHeight: 8000, BitDepth: 16, ExposureScore: .9, FocusScore: .2})
	if err != nil {
		t.Fatal(err)
	}
	evaluated, err := s.Evaluate(ctx, b.ID, EvaluateRequest{CommandMeta: CommandMeta{RequestID: "evaluate-bad", ExpectedRevision: added.Batch.Revision, Actor: "operator"}})
	if err != nil {
		t.Fatal(err)
	}
	issueID := evaluated.Batch.Issues[0].ID
	resolved, err := s.ResolveIssue(ctx, b.ID, issueID, ResolveIssueRequest{CommandMeta: CommandMeta{RequestID: "accept-issue", ExpectedRevision: evaluated.Batch.Revision, Actor: "reviewer"}, ResolutionKind: "accepted", ResolutionNote: "经复核可接受"})
	if err != nil {
		t.Fatal(err)
	}
	req := ResolutionRevocationRequest{CommandMeta: CommandMeta{RequestID: "revoke-resolution", ExpectedRevision: resolved.Batch.Revision, Actor: "reviewer"}, Reason: "裁决依据录入错误"}
	revoked, err := s.RevokeIssueResolution(ctx, b.ID, issueID, req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.RevokeIssueResolution(ctx, b.ID, issueID, req)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Replayed || revoked.Batch.Issues[0].Status != "open" || len(revoked.Batch.Issues[0].ResolutionHistory) != 1 || revoked.Batch.Issues[0].ResolutionHistory[0].RevokedAt == nil {
		t.Fatalf("撤销历史错误: %#v", revoked.Batch.Issues[0])
	}
	_, err = s.RequestPeerReview(ctx, b.ID, PeerReviewRequestRequest{CommandMeta: CommandMeta{RequestID: "blocked-peer", ExpectedRevision: revoked.Batch.Revision, Actor: "reviewer"}})
	if err == nil {
		t.Fatal("开放问题未阻断同行抽验")
	}
	second, err := s.ResolveIssue(ctx, b.ID, issueID, ResolveIssueRequest{CommandMeta: CommandMeta{RequestID: "accept-again", ExpectedRevision: revoked.Batch.Revision, Actor: "reviewer"}, ResolutionKind: "accepted", ResolutionNote: "重新核对后接受"})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Batch.Issues[0].ResolutionHistory) != 2 {
		t.Fatalf("重新裁决未追加历史: %#v", second.Batch.Issues[0])
	}
}

func TestPeerWorkItemAndManifestReconciliationAreReadOnly(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	b := createCapturingBatch(t, s, "archive-batch", 10, 10)
	b = addPassingScan(t, s, b.ID, "archive-scan", b.Revision, 10, "operator")
	evaluated, err := s.Evaluate(ctx, b.ID, EvaluateRequest{CommandMeta: CommandMeta{RequestID: "archive-evaluate", ExpectedRevision: b.Revision, Actor: "operator"}})
	if err != nil {
		t.Fatal(err)
	}
	requested, err := s.RequestPeerReview(ctx, b.ID, PeerReviewRequestRequest{CommandMeta: CommandMeta{RequestID: "request-peer", ExpectedRevision: evaluated.Batch.Revision, Actor: "operator"}})
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.PeerReviewWorkItem(ctx, b.ID, "independent-reviewer", requested.Batch.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !item.Eligible || len(item.Samples) != 1 || item.EvidenceSubmitRevision != requested.Batch.Revision {
		t.Fatalf("任务单错误: %#v", item)
	}
	blocked, err := s.PeerReviewWorkItem(ctx, b.ID, "operator", requested.Batch.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Eligible {
		t.Fatal("采集人被判定为可复核")
	}
	evidence := PeerReviewEvidenceRequest{CommandMeta: CommandMeta{RequestID: "peer-evidence", ExpectedRevision: item.EvidenceSubmitRevision, Actor: "independent-reviewer"}, Evidence: []PeerReviewEvidenceItem{{CatalogNumber: 10, ObservedChecksum: item.Samples[0].ContentChecksum, DimensionsMatch: true, BitDepthMatch: true}}}
	reviewed, err := s.SubmitPeerReviewEvidence(ctx, b.ID, evidence)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal(ctx, b.ID, SealRequest{CommandMeta: CommandMeta{RequestID: "seal-batch", ExpectedRevision: reviewed.Batch.Revision, Actor: "curator"}})
	if err != nil {
		t.Fatal(err)
	}
	entry := sealed.Manifest.Entries[0]
	actual := domain.ManifestActualEntry{CatalogNumber: entry.CatalogNumber, ContentChecksum: entry.ContentChecksum, PixelWidth: entry.PixelWidth, PixelHeight: entry.PixelHeight, BitDepth: entry.BitDepth}
	matched, err := s.ReconcileManifest(ctx, b.ID, ManifestReconcileRequest{ManifestHash: sealed.Manifest.ManifestHash, Entries: []domain.ManifestActualEntry{actual}})
	if err != nil {
		t.Fatal(err)
	}
	if !matched.Matched {
		t.Fatalf("一致成果核验失败: %#v", matched)
	}
	actual.BitDepth = 8
	different, err := s.ReconcileManifest(ctx, b.ID, ManifestReconcileRequest{ManifestHash: sealed.Manifest.ManifestHash, Entries: []domain.ManifestActualEntry{actual}})
	if err != nil {
		t.Fatal(err)
	}
	if different.Matched || different.MetadataMismatchCount != 1 {
		t.Fatalf("元数据差异分类错误: %#v", different)
	}
	after, err := s.GetBatch(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != sealed.Batch.Revision {
		t.Fatal("只读查询改变了批次修订")
	}
}

func TestIdempotencyAndRevisionConflict(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	create := CreateBatchRequest{CommandMeta: CommandMeta{RequestID: "req-create", Actor: "operator"}, ID: "batch-test", Title: "测试", CatalogStart: 1, CatalogEnd: 1, ScannerID: "scanner", QualityPolicyVersion: "v1"}
	first, err := s.CreateBatch(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateBatch(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Replayed || again.Batch.ID != first.Batch.ID {
		t.Fatalf("幂等重放错误: %#v", again)
	}
	changed := create
	changed.Title = "不同载荷"
	if _, err = s.CreateBatch(ctx, changed); err == nil {
		t.Fatal("同 request_id 不同载荷未冲突")
	}
	cal := CalibrationRequest{CommandMeta: CommandMeta{RequestID: "req-cal", ExpectedRevision: 99, Actor: "operator"}, ResolutionDPI: 3200, GrayResponseError: .01, GeometryErrorPercent: .05}
	_, err = s.Calibrate(ctx, create.ID, cal)
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeConflict || de.CurrentRevision != 1 {
		t.Fatalf("修订冲突错误不完整: %v", err)
	}
}
