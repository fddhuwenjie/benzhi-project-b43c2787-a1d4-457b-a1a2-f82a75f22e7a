package corrupt_snapshot_error_chain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
	_ "modernc.org/sqlite"
)

func TestCorruptSnapshotPreservesIntegrityError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	store, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(store)
	_, err = service.CreateBatch(context.Background(), application.CreateBatchRequest{
		CommandMeta:          application.CommandMeta{RequestID: "create-corrupt", Actor: "operator"},
		ID:                   "batch-corrupt",
		Title:                "损坏快照",
		CatalogStart:         1,
		CatalogEnd:           1,
		ScannerID:            "scanner",
		QualityPolicyVersion: "v1",
	})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = db.QueryRow(`SELECT snapshot FROM plate_batches WHERE id=?`, "batch-corrupt").Scan(&raw); err != nil {
		db.Close()
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		db.Close()
		t.Fatal(err)
	}
	snapshot["state"] = "corrupt_state"
	corrupted, err := json.Marshal(snapshot)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE plate_batches SET snapshot=? WHERE id=?`, corrupted, "batch-corrupt"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = application.NewService(store).GetBatch(context.Background(), "batch-corrupt")
	var domainErr *domain.Error
	if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeIntegrity {
		t.Fatalf("损坏快照未保留 integrity 错误码: %v", err)
	}
}
