package draft_actor_confusion

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

func TestPeerReviewDraftCompletionRequiresOwnerActor(t *testing.T) {
	store, err := persistence.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	s := application.NewService(store)
	ctx := context.Background()
	created, err := s.CreateBatch(ctx, application.CreateBatchRequest{CommandMeta: application.CommandMeta{RequestID: "create", Actor: "operator"}, ID: "draft-owner-batch", Title: "draft owner", CatalogStart: 1, CatalogEnd: 1, ScannerID: "scanner", QualityPolicyVersion: "v1"})
	if err != nil { t.Fatal(err) }
	cal, err := s.Calibrate(ctx, created.Batch.ID, application.CalibrationRequest{CommandMeta: application.CommandMeta{RequestID: "cal", ExpectedRevision: created.Batch.Revision, Actor: "operator"}, ResolutionDPI: 3200, GrayResponseError: .01, GeometryErrorPercent: .01})
	if err != nil { t.Fatal(err) }
	scan, err := s.AddScan(ctx, created.Batch.ID, application.ScanRequest{CommandMeta: application.CommandMeta{RequestID: "scan", ExpectedRevision: cal.Batch.Revision, Actor: "operator"}, CatalogNumber: 1, ContentChecksum: "checksum-owner-scan", PixelWidth: 8000, PixelHeight: 8000, BitDepth: 16, ExposureScore: .9, FocusScore: .9})
	if err != nil { t.Fatal(err) }
	evaluated, err := s.Evaluate(ctx, created.Batch.ID, application.EvaluateRequest{CommandMeta: application.CommandMeta{RequestID: "eval", ExpectedRevision: scan.Batch.Revision, Actor: "operator"}})
	if err != nil { t.Fatal(err) }
	reviewing, err := s.RequestPeerReview(ctx, created.Batch.ID, application.PeerReviewRequestRequest{CommandMeta: application.CommandMeta{RequestID: "request-review", ExpectedRevision: evaluated.Batch.Revision, Actor: "operator"}})
	if err != nil { t.Fatal(err) }
	drafted, err := s.CreatePeerReviewDraft(ctx, created.Batch.ID, application.CreatePeerReviewDraftRequest{CommandMeta: application.CommandMeta{RequestID: "create-draft", ExpectedRevision: reviewing.Batch.Revision, Actor: "reviewer-a"}})
	if err != nil { t.Fatal(err) }
	draft := drafted.PeerReviewDraft.Draft
	sample := draft.Samples[0]
	intruderPut, putErr := s.PutPeerReviewDraftEvidence(ctx, created.Batch.ID, draft.ID, application.PutPeerReviewDraftEvidenceRequest{CommandMeta: application.CommandMeta{RequestID: "put-by-other", ExpectedRevision: reviewing.Batch.Revision, Actor: "reviewer-b"}, ExpectedDraftRevision: draft.DraftRevision, CatalogNumber: sample.CatalogNumber, ScanID: sample.ScanID, Version: sample.Version, ObservedChecksum: sample.ContentChecksum, DimensionsMatch: true, BitDepthMatch: true})
	currentDraftRevision := draft.DraftRevision
	if intruderPut.PeerReviewDraft != nil { currentDraftRevision = intruderPut.PeerReviewDraft.Draft.DraftRevision }
	put, err := s.PutPeerReviewDraftEvidence(ctx, created.Batch.ID, draft.ID, application.PutPeerReviewDraftEvidenceRequest{CommandMeta: application.CommandMeta{RequestID: "put-by-owner", ExpectedRevision: reviewing.Batch.Revision, Actor: "reviewer-a"}, ExpectedDraftRevision: currentDraftRevision, CatalogNumber: sample.CatalogNumber, ScanID: sample.ScanID, Version: sample.Version, ObservedChecksum: sample.ContentChecksum, DimensionsMatch: true, BitDepthMatch: true})
	if err != nil { t.Fatal(err) }
	result, completeErr := s.CompletePeerReviewDraft(ctx, created.Batch.ID, draft.ID, application.CompletePeerReviewDraftRequest{CommandMeta: application.CommandMeta{RequestID: "complete-by-other", ExpectedRevision: reviewing.Batch.Revision, Actor: "reviewer-b"}, ExpectedDraftRevision: put.PeerReviewDraft.Draft.DraftRevision})
	var putBusiness, completeBusiness *domain.Error
	putForbidden := errors.As(putErr, &putBusiness) && putBusiness.Code == domain.CodeForbidden
	completeForbidden := errors.As(completeErr, &completeBusiness) && completeBusiness.Code == domain.CodeForbidden
	if !putForbidden || !completeForbidden {
		recorded := "<none>"
		if result.Batch != nil && len(result.Batch.PeerReviews) > 0 { recorded = result.Batch.PeerReviews[len(result.Batch.PeerReviews)-1].Reviewer }
		t.Fatalf("TestPeerReviewDraftCompletionRequiresOwnerActor: non-owner mutated/completed draft while review was attributed to %s: putErr=%v completeErr=%v", recorded, putErr, completeErr)
	}
}
