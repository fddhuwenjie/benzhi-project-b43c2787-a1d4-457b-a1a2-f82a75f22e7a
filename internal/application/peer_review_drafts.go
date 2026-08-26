package application

import (
	"context"

	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

func (s *Service) CreatePeerReviewDraft(ctx context.Context, batchID string, r CreatePeerReviewDraftRequest) (CommandResult, error) {
	if err := validateMeta(r.CommandMeta, false); err != nil {
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
			result, e = replayOrConflict(record, "create_peer_review_draft", hash)
			return e
		}
		batch, e := tx.LoadBatch(ctx, batchID)
		if e != nil {
			return e
		}
		if batch.Revision != r.ExpectedRevision {
			return domain.RevisionConflict(batch.Revision)
		}
		if existing, e := tx.FindOpenPeerReviewDraft(ctx, batchID, len(batch.PeerReviews)+1, r.Actor); e != nil {
			return e
		} else if existing != nil {
			return domain.NewError(domain.CodeDuplicate, "本轮该复核员已有开放草稿 %s", existing.ID)
		}
		draft, e := domain.NewPeerReviewDraft(newID("draft"), batch, r.Actor, s.now())
		if e != nil {
			return e
		}
		if e = tx.InsertPeerReviewDraft(ctx, draft); e != nil {
			return e
		}
		if e = appendEvent(ctx, tx, batch, "peer_review.draft_created", r.Actor, map[string]any{"request_id": r.RequestID, "draft_id": draft.ID, "round": draft.Round, "samples": draft.Samples}, s.now()); e != nil {
			return e
		}
		result = CommandResult{Batch: batch, PeerReviewDraft: draftView(draft, batch.Revision)}
		return putResult(ctx, tx, r.RequestID, batchID, "create_peer_review_draft", hash, result)
	})
	return result, err
}

func (s *Service) PutPeerReviewDraftEvidence(ctx context.Context, batchID, draftID string, r PutPeerReviewDraftEvidenceRequest) (CommandResult, error) {
	if err := validateMeta(r.CommandMeta, false); err != nil {
		return CommandResult{}, err
	}
	if r.ExpectedDraftRevision < 1 {
		return CommandResult{}, domain.NewError(domain.CodeValidation, "expected_draft_revision 必须为正整数")
	}
	if err := domain.ValidateIdentifier("draft_id", draftID); err != nil {
		return CommandResult{}, err
	}
	hash, err := payloadHash(r)
	if err != nil {
		return CommandResult{}, err
	}
	operation := "put_peer_review_draft_evidence:" + draftID
	unlock := s.lock(batchID)
	defer unlock()
	var result CommandResult
	err = s.store.WithTx(ctx, func(tx *persistence.Tx) error {
		record, e := tx.GetIdempotency(ctx, r.RequestID)
		if e != nil {
			return e
		}
		if record != nil {
			result, e = replayOrConflict(record, operation, hash)
			return e
		}
		batch, e := tx.LoadBatch(ctx, batchID)
		if e != nil {
			return e
		}
		if batch.Revision != r.ExpectedRevision {
			return domain.RevisionConflict(batch.Revision)
		}
		draft, e := tx.LoadPeerReviewDraft(ctx, batchID, draftID)
		if e != nil {
			return e
		}
		if e = domain.ValidatePeerReviewDraft(draft, batch, true); e != nil {
			return e
		}
		if draft.DraftRevision != r.ExpectedDraftRevision {
			return domain.NewError(domain.CodeConflict, "expected_draft_revision 与当前草稿修订 %d 不一致", draft.DraftRevision)
		}
		stored := draft.DraftRevision
		changed, e := draft.PutEvidence(r.CatalogNumber, r.ScanID, r.Version, r.ObservedChecksum, r.DimensionsMatch, r.BitDepthMatch, r.Note, s.now())
		if e != nil {
			return e
		}
		if e = tx.SavePeerReviewDraft(ctx, draft, stored); e != nil {
			return e
		}
		if e = appendEvent(ctx, tx, batch, "peer_review.draft_evidence_changed", r.Actor, map[string]any{"request_id": r.RequestID, "draft_id": draft.ID, "catalog_number": r.CatalogNumber, "changed": changed, "completed_catalogs": sortedEvidenceCatalogs(draft)}, s.now()); e != nil {
			return e
		}
		result = CommandResult{Batch: batch, PeerReviewDraft: draftView(draft, batch.Revision)}
		return putResult(ctx, tx, r.RequestID, batchID, operation, hash, result)
	})
	return result, err
}

func (s *Service) GetPeerReviewDraft(ctx context.Context, batchID, draftID string) (PeerReviewDraftView, error) {
	if err := domain.ValidateIdentifier("draft_id", draftID); err != nil {
		return PeerReviewDraftView{}, err
	}
	unlock := s.lock(batchID)
	defer unlock()
	batch, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return PeerReviewDraftView{}, err
	}
	draft, err := s.store.LoadPeerReviewDraft(ctx, batchID, draftID)
	if err != nil {
		return PeerReviewDraftView{}, err
	}
	if err = domain.ValidatePeerReviewDraft(draft, batch, true); err != nil {
		return PeerReviewDraftView{}, err
	}
	return *draftView(draft, batch.Revision), nil
}

func (s *Service) CompletePeerReviewDraft(ctx context.Context, batchID, draftID string, r CompletePeerReviewDraftRequest) (CommandResult, error) {
	if err := validateMeta(r.CommandMeta, false); err != nil {
		return CommandResult{}, err
	}
	if r.ExpectedDraftRevision < 1 {
		return CommandResult{}, domain.NewError(domain.CodeValidation, "expected_draft_revision 必须为正整数")
	}
	if err := domain.ValidateIdentifier("draft_id", draftID); err != nil {
		return CommandResult{}, err
	}
	hash, err := payloadHash(r)
	if err != nil {
		return CommandResult{}, err
	}
	operation := "complete_peer_review_draft:" + draftID
	unlock := s.lock(batchID)
	defer unlock()
	var result CommandResult
	err = s.store.WithTx(ctx, func(tx *persistence.Tx) error {
		record, e := tx.GetIdempotency(ctx, r.RequestID)
		if e != nil {
			return e
		}
		if record != nil {
			result, e = replayOrConflict(record, operation, hash)
			return e
		}
		batch, e := tx.LoadBatch(ctx, batchID)
		if e != nil {
			return e
		}
		if batch.Revision != r.ExpectedRevision {
			return domain.RevisionConflict(batch.Revision)
		}
		draft, e := tx.LoadPeerReviewDraft(ctx, batchID, draftID)
		if e != nil {
			return e
		}
		if draft.DraftRevision != r.ExpectedDraftRevision {
			return domain.NewError(domain.CodeConflict, "expected_draft_revision 与当前草稿修订 %d 不一致", draft.DraftRevision)
		}
		storedBatch, storedDraft := batch.Revision, draft.DraftRevision
		if e = draft.Complete(batch, func() string { return newID("issue") }, s.now()); e != nil {
			return e
		}
		if e = tx.SaveBatch(ctx, batch, storedBatch); e != nil {
			return e
		}
		if e = tx.SavePeerReviewDraft(ctx, draft, storedDraft); e != nil {
			return e
		}
		review := batch.PeerReviews[len(batch.PeerReviews)-1]
		failureIssues := []string{}
		for _, issue := range batch.Issues {
			if issue.Status == "open" && issue.RuleCode == "peer_review_failure" {
				failureIssues = append(failureIssues, issue.ID)
			}
		}
		if e = appendEvent(ctx, tx, batch, "peer_review.draft_completed", r.Actor, map[string]any{"request_id": r.RequestID, "draft_id": draft.ID, "passed": review.Passed, "evidence": review.Evidence, "failure_issue_ids": failureIssues}, s.now()); e != nil {
			return e
		}
		result = CommandResult{Batch: batch, PeerReviewDraft: draftView(draft, batch.Revision)}
		return putResult(ctx, tx, r.RequestID, batchID, operation, hash, result)
	})
	return result, err
}
