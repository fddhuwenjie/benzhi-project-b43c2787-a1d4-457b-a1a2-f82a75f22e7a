package idempotency_cross_batch

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

func TestIdempotencyReplayMustBindBatch(t *testing.T) {
	store, err := persistence.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := application.NewService(store)
	ctx := context.Background()
	for _, id := range []string{"batch-a", "batch-b"} {
		_, err = service.CreateBatch(ctx, application.CreateBatchRequest{
			CommandMeta: application.CommandMeta{RequestID: "create-" + id, Actor: "operator"},
			ID: id, Title: id, CatalogStart: 1, CatalogEnd: 1, ScannerID: "scanner", QualityPolicyVersion: "v1",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	request := application.CalibrationRequest{
		CommandMeta: application.CommandMeta{RequestID: "shared-calibration", ExpectedRevision: 1, Actor: "operator"},
		ResolutionDPI: 3200, GrayResponseError: 0.01, GeometryErrorPercent: 0.01,
	}
	if _, err = service.Calibrate(ctx, "batch-a", request); err != nil {
		t.Fatal(err)
	}
	result, err := service.Calibrate(ctx, "batch-b", request)
	var business *domain.Error
	if !errors.As(err, &business) || business.Code != domain.CodeIdempotency {
		t.Fatalf("TestIdempotencyReplayMustBindBatch: second resource reused first response: batch=%v replayed=%v err=%v", func() string {
			if result.Batch == nil { return "<nil>" }
			return result.Batch.ID
		}(), result.Replayed, err)
	}
}
