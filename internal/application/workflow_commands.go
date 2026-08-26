package application

import (
	"context"

	"astroplate-vault/internal/audit"
	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

func (s *Service) Evaluate(ctx context.Context, batchID string, r EvaluateRequest) (CommandResult, error) {
	return s.mutate(ctx, batchID, "evaluate", "quality.evaluated", r.CommandMeta, r, func(b *domain.PlateBatch) (any, error) { return b.Evaluate(func() string { return newID("issue") }) })
}

func (s *Service) ResolveIssue(ctx context.Context, batchID, issueID string, r ResolveIssueRequest) (CommandResult, error) {
	return s.mutate(ctx, batchID, "resolve_issue:"+issueID, "issue.resolved", r.CommandMeta, r, func(b *domain.PlateBatch) (any, error) {
		if err := b.ResolveIssue(issueID, r.ResolutionKind, r.ResolutionNote, r.ReplacementScanID, r.Actor, s.now()); err != nil {
			return nil, err
		}
		return map[string]any{"issue_id": issueID, "resolution_kind": r.ResolutionKind, "replacement_scan_id": r.ReplacementScanID}, nil
	})
}

func (s *Service) RequestPeerReview(ctx context.Context, batchID string, r PeerReviewRequestRequest) (CommandResult, error) {
	return s.mutate(ctx, batchID, "request_peer_review", "peer_review.requested", r.CommandMeta, r, func(b *domain.PlateBatch) (any, error) {
		if err := b.RequestPeerReview(); err != nil {
			return nil, err
		}
		return map[string]any{"sample_catalogs": b.SampleCatalogs()}, nil
	})
}

func (s *Service) SubmitPeerReview(ctx context.Context, batchID string, r PeerReviewRequest) (CommandResult, error) {
	return s.mutate(ctx, batchID, "submit_peer_review", "peer_review.completed", r.CommandMeta, r, func(b *domain.PlateBatch) (any, error) {
		if err := b.RecordPeerReview(r.Actor, r.SampleCatalogs, r.Passed, r.Note, newID("issue"), s.now()); err != nil {
			return nil, err
		}
		return b.PeerReviews[len(b.PeerReviews)-1], nil
	})
}

func (s *Service) Seal(ctx context.Context, batchID string, r SealRequest) (CommandResult, error) {
	if err := validateMeta(r.CommandMeta, false); err != nil {
		return CommandResult{}, err
	}
	hash, err := payloadHash(r)
	if err != nil {
		return CommandResult{}, err
	}
	unlock := s.lock(batchID)
	defer unlock()
	record, err := s.store.GetIdempotency(ctx, r.RequestID)
	if err != nil {
		return CommandResult{}, err
	}
	if record != nil {
		return s.replayOrConflict(record, "seal", hash)
	}
	verified, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return CommandResult{}, err
	}
	if verified.Revision != r.ExpectedRevision {
		return CommandResult{}, domain.RevisionConflict(verified.Revision)
	}
	var result CommandResult
	err = s.store.WithTx(ctx, func(tx *persistence.Tx) error {
		record, err := tx.GetIdempotency(ctx, r.RequestID)
		if err != nil {
			return err
		}
		if record != nil {
			result, err = s.replayOrConflict(record, "seal", hash)
			return err
		}
		batch, err := tx.LoadBatch(ctx, batchID)
		if err != nil {
			return err
		}
		if batch.Revision != r.ExpectedRevision {
			return domain.RevisionConflict(batch.Revision)
		}
		events, err := tx.AllAudit(ctx, batchID)
		if err != nil {
			return err
		}
		if err = audit.Verify(events); err != nil {
			return err
		}
		if err = tx.ValidateProjectionCounts(ctx, batch); err != nil {
			return err
		}
		if blockers := domain.EnsureArchiveBusinessReady(batch); len(blockers) > 0 {
			return domain.NewError(domain.CodeInvalidState, "批次尚未通过封存就绪检查：%v", blockers)
		}
		last, err := tx.LastAudit(ctx, batchID)
		if err != nil {
			return err
		}
		stored := batch.Revision
		manifest, err := batch.BuildManifest(last.EventHash, r.Actor, s.now())
		if err != nil {
			return err
		}
		if err = tx.SaveBatch(ctx, batch, stored); err != nil {
			return err
		}
		if err = tx.PutManifest(ctx, manifest); err != nil {
			return err
		}
		if err = appendEvent(ctx, tx, batch, "batch.sealed", r.Actor, map[string]any{"request_id": r.RequestID, "manifest_hash": manifest.ManifestHash, "captured_audit_head": manifest.AuditHeadHash}, s.now()); err != nil {
			return err
		}
		result = CommandResult{Batch: batch, Manifest: &manifest}
		return putResult(ctx, tx, r.RequestID, batchID, "seal", hash, result)
	})
	return result, err
}
