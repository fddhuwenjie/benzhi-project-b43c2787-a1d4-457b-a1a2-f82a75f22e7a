package application

import (
	"context"

	"fmt"

	"astroplate-vault/internal/audit"
	"astroplate-vault/internal/domain"
)

func (s *Service) GetBatch(ctx context.Context, batchID string) (*domain.PlateBatch, error) {
	events, err := s.store.AllAudit(ctx, batchID)
	if err != nil {
		return nil, err
	}
	if err = audit.Verify(events); err != nil {
		return nil, err
	}
	batch, err := s.store.LoadBatch(ctx, batchID)
	if err != nil {
		return nil, err
	}
	if err = s.store.ValidateBatchProjection(ctx, batch); err != nil {
		return nil, err
	}
	return batch, nil
}

func (s *Service) GetProjection(ctx context.Context, batchID string) (BatchProjection, error) {
	batch, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return BatchProjection{}, err
	}
	sample := []int{}
	if batch.State == domain.StatePeerReview {
		sample = batch.SampleCatalogs()
	}
	return BatchProjection{Batch: batch, ExpectedPlateCount: batch.CatalogEnd - batch.CatalogStart + 1, ActiveScanCount: len(batch.ActiveScans()), MissingCatalogs: domain.MissingCatalogs(batch), OpenIssueCount: domain.OpenIssueCount(batch), PeerReviewSample: sample, Writable: batch.State != domain.StateSealed, Description: domain.AggregateDescription(batch)}, nil
}

func (s *Service) GetAudit(ctx context.Context, batchID string, after int64, limit int) (AuditPage, error) {
	if _, err := s.store.LoadBatch(ctx, batchID); err != nil {
		return AuditPage{}, err
	}
	all, err := s.store.AllAudit(ctx, batchID)
	if err != nil {
		return AuditPage{}, err
	}
	if err = audit.Verify(all); err != nil {
		return AuditPage{HeadHash: audit.Head(all), Verified: false, FirstInvalidSequence: firstInvalid(all), IntegrityDetail: err.Error()}, err
	}
	events, err := s.store.ListAudit(ctx, batchID, after, limit)
	if err != nil {
		return AuditPage{}, err
	}
	next := after
	if len(events) > 0 {
		next = events[len(events)-1].Sequence
	}
	return AuditPage{Events: events, HeadHash: audit.Head(all), NextAfter: next, Verified: true}, nil
}

func (s *Service) GetAuditFiltered(ctx context.Context, batchID, eventType string, after, before int64, limit int) (AuditPage, error) {
	if _, err := s.store.LoadBatch(ctx, batchID); err != nil {
		return AuditPage{}, err
	}
	all, e := s.store.AllAudit(ctx, batchID)
	if e != nil {
		return AuditPage{}, e
	}
	if e = audit.Verify(all); e != nil {
		return AuditPage{HeadHash: audit.Head(all), Verified: false, FirstInvalidSequence: firstInvalid(all), IntegrityDetail: e.Error()}, e
	}
	ev, e := s.store.ListAuditFiltered(ctx, batchID, eventType, after, before, limit)
	if e != nil {
		return AuditPage{}, e
	}
	n := after
	if len(ev) > 0 {
		n = ev[len(ev)-1].Sequence
	}
	return AuditPage{Events: ev, HeadHash: audit.Head(all), NextAfter: n, Verified: true}, nil
}

func firstInvalid(events []domain.AuditEvent) int64 {
	for i := 1; i <= len(events); i++ {
		if err := audit.Verify(events[:i]); err != nil {
			return int64(i)
		}
	}
	return 0
}

func (s *Service) GetManifest(ctx context.Context, batchID string) (domain.ArchiveManifest, error) {
	return s.store.LoadManifest(ctx, batchID)
}

func (s *Service) VerifyManifest(ctx context.Context, batchID, claimedHash string) (ManifestVerification, error) {
	m, err := s.store.LoadManifest(ctx, batchID)
	if err != nil {
		return ManifestVerification{}, err
	}
	events, err := s.store.AllAudit(ctx, batchID)
	if err != nil {
		return ManifestVerification{}, err
	}
	if err = audit.Verify(events); err != nil {
		return ManifestVerification{}, err
	}
	present := len(events) >= 2 && events[len(events)-2].EventHash == m.AuditHeadHash && events[len(events)-1].EventType == "batch.sealed"
	valid := domain.VerifyManifest(m) && present && (claimedHash == "" || claimedHash == m.ManifestHash)
	return ManifestVerification{Valid: valid, ManifestHash: m.ManifestHash, AuditHeadPresent: present}, nil
}

func (s *Service) ValidateAuditChains(ctx context.Context) error {
	ids, err := s.store.BatchIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		events, err := s.store.AllAudit(ctx, id)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return domain.NewError(domain.CodeIntegrity, "批次 %s 缺少审计事件", id)
		}
		if err = audit.Verify(events); err != nil {
			return fmt.Errorf("批次 %s: %w", id, err)
		}
		batch, err := s.store.LoadBatch(ctx, id)
		if err != nil {
			return err
		}
		if err = s.store.ValidateBatchProjection(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}
