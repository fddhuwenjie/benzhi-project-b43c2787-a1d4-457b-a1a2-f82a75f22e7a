package lockregistryleak_test

import (
	"context"
	"fmt"
	"testing"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/persistence"
)

func TestLockRegistryReclaimsEntries(t *testing.T) {
	store, err := persistence.Open(t.TempDir() + "/vault.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := application.NewService(store)
	for i := 0; i < 32; i++ {
		id := fmt.Sprintf("batch-%04d", i)
		_, err := svc.CreateBatch(context.Background(), application.CreateBatchRequest{
			CommandMeta: application.CommandMeta{RequestID: fmt.Sprintf("request-%04d", i), Actor: "operator"},
			ID: id, Title: "锁生命周期复现", CatalogStart: 1, CatalogEnd: 1, ScannerID: "scanner-1", QualityPolicyVersion: "v1",
		})
		if err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	count := svc.LockRegistrySize()
	if count != 0 {
		t.Fatalf("TestLockRegistryReclaimsEntries: lock registry retained %d entries after requests", count)
	}
}
