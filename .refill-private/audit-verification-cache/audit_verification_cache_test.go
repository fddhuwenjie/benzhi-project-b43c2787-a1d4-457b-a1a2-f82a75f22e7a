package auditverificationcache_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/httpapi"
	"astroplate-vault/internal/persistence"
	_ "modernc.org/sqlite"
)

func TestAuditQueryMustReverifyAfterPersistentResourceChanges(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "audit-cache.db")
	store, err := persistence.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	service := application.NewService(store)
	created, err := service.CreateBatch(context.Background(), application.CreateBatchRequest{
		CommandMeta: application.CommandMeta{RequestID: "create-audit-cache", Actor: "creator"},
		ID:          "audit-cache-batch", Title: "审计缓存资源失效复现", CatalogStart: 1, CatalogEnd: 1,
		ScannerID: "scanner-1", QualityPolicyVersion: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	handler := httpapi.New(service).Handler()
	queryAudit := func() *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/plate-batches/"+created.Batch.ID+"/audit-events?limit=50", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if first := queryAudit(); first.Code != http.StatusOK {
		t.Fatalf("首次审计查询返回 %d: %s", first.Code, first.Body.String())
	}

	external, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = external.Close() })
	result, err := external.ExecContext(context.Background(),
		`UPDATE audit_events SET payload=? WHERE batch_id=? AND sequence=1`,
		[]byte(`{"request_id":"tampered-after-verification"}`), created.Batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("预期篡改一条审计事件，实际修改 %d 条", changed)
	}

	second := queryAudit()
	if second.Code != http.StatusInternalServerError {
		t.Fatalf("持久化审计载荷变化后查询仍返回 %d，未重新校验哈希链: %s", second.Code, second.Body.String())
	}
}
