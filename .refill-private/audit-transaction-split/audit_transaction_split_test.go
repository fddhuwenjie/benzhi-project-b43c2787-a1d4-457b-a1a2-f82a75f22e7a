package audit_transaction_split_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/persistence"
	_ "modernc.org/sqlite"
)

func TestMutationRollsBackWhenAuditAppendFails(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "audit-atomicity.db")
	store, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := application.NewService(store)

	created, err := service.CreateBatch(ctx, application.CreateBatchRequest{
		CommandMeta:          application.CommandMeta{RequestID: "create-audit-atomicity", Actor: "creator"},
		ID:                   "audit-atomicity-batch",
		Title:                "审计事务原子性复现",
		CatalogStart:         1,
		CatalogEnd:           1,
		ScannerID:            "scanner-a",
		QualityPolicyVersion: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	calibrated, err := service.Calibrate(ctx, created.Batch.ID, application.CalibrationRequest{
		CommandMeta:          application.CommandMeta{RequestID: "calibrate-audit-atomicity", ExpectedRevision: created.Batch.Revision, Actor: "operator"},
		ResolutionDPI:        3200,
		GrayResponseError:    0.01,
		GeometryErrorPercent: 0.05,
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeRevision := calibrated.Batch.Revision

	injector, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = injector.ExecContext(ctx, `CREATE TRIGGER reject_scan_audit
		BEFORE INSERT ON audit_events
		WHEN NEW.event_type = 'scan.registered'
		BEGIN
			SELECT RAISE(ABORT, 'forced audit append failure');
		END`)
	if closeErr := injector.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	_, commandErr := service.AddScan(ctx, created.Batch.ID, application.ScanRequest{
		CommandMeta:     application.CommandMeta{RequestID: "scan-audit-atomicity", ExpectedRevision: beforeRevision, Actor: "operator"},
		CatalogNumber:   1,
		ContentChecksum: "checksum-audit-atomicity",
		PixelWidth:      8000,
		PixelHeight:     8000,
		BitDepth:        16,
		ExposureScore:   0.9,
		FocusScore:      0.95,
	})
	if commandErr == nil {
		t.Fatal("预期审计写入失败，命令却成功")
	}

	after, err := service.GetBatch(ctx, created.Batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != beforeRevision || len(after.ActiveScans()) != 0 {
		t.Fatalf("失败命令未整体回滚：修订从 %d 变为 %d，活动扫描数为 %d", beforeRevision, after.Revision, len(after.ActiveScans()))
	}
}
