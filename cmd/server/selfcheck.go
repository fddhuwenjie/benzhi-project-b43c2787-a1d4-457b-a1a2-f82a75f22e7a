package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/httpapi"
	"astroplate-vault/internal/persistence"
)

type selfCheckClient struct {
	base   string
	client *http.Client
}

func runSelfCheck(cfg config) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.selfCheckTimeout)
	defer cancel()
	temp, err := os.MkdirTemp("", "astroplate-vault-self-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	store, err := persistence.Open(filepath.Join(temp, "self-check.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("自检监听 %s: %w", cfg.addr, err)
	}
	server := newServer(cfg.addr, store)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	client := selfCheckClient{base: "http://" + listener.Addr().String(), client: &http.Client{Timeout: 5 * time.Second}}
	if err = client.workflow(ctx); err != nil {
		return err
	}
	fmt.Println("self-check passed: 创建、标定、采集、评估、抽验、封存、清单与审计核验完成")
	return nil
}

func (c selfCheckClient) workflow(ctx context.Context) error {
	create := application.CreateBatchRequest{CommandMeta: application.CommandMeta{RequestID: "self-create", ExpectedRevision: 0, Actor: "operator-a"}, ID: "self-check-batch", Title: "自检底片批次", CatalogStart: 1001, CatalogEnd: 1001, ScannerID: "scanner-self", QualityPolicyVersion: "v1"}
	var result application.CommandResult
	if err := c.post(ctx, "/api/v1/plate-batches", create, &result); err != nil {
		return err
	}
	rev := result.Batch.Revision
	cal := application.CalibrationRequest{CommandMeta: application.CommandMeta{RequestID: "self-cal", ExpectedRevision: rev, Actor: "operator-a"}, ResolutionDPI: 3200, GrayResponseError: 0.01, GeometryErrorPercent: 0.05}
	if err := c.post(ctx, httpapi.URL(c.base, create.ID, "/calibrations"), cal, &result); err != nil {
		return err
	}
	rev = result.Batch.Revision
	scan := application.ScanRequest{CommandMeta: application.CommandMeta{RequestID: "self-scan", ExpectedRevision: rev, Actor: "operator-a"}, CatalogNumber: 1001, ContentChecksum: "sha256:0123456789abcdef", PixelWidth: 8000, PixelHeight: 8000, BitDepth: 16, ExposureScore: 0.95, FocusScore: 0.96}
	if err := c.post(ctx, httpapi.URL(c.base, create.ID, "/scans"), scan, &result); err != nil {
		return err
	}
	rev = result.Batch.Revision
	evaluate := application.EvaluateRequest{CommandMeta: application.CommandMeta{RequestID: "self-eval", ExpectedRevision: rev, Actor: "operator-a"}}
	if err := c.post(ctx, httpapi.URL(c.base, create.ID, "/quality-evaluations"), evaluate, &result); err != nil {
		return err
	}
	rev = result.Batch.Revision
	requestReview := application.PeerReviewRequestRequest{CommandMeta: application.CommandMeta{RequestID: "self-request-review", ExpectedRevision: rev, Actor: "operator-a"}}
	if err := c.post(ctx, httpapi.URL(c.base, create.ID, "/peer-review-request"), requestReview, &result); err != nil {
		return err
	}
	rev = result.Batch.Revision
	review := application.PeerReviewRequest{CommandMeta: application.CommandMeta{RequestID: "self-review", ExpectedRevision: rev, Actor: "reviewer-b"}, SampleCatalogs: []int{1001}, Passed: true, Note: "样本核验通过"}
	if err := c.post(ctx, httpapi.URL(c.base, create.ID, "/peer-reviews"), review, &result); err != nil {
		return err
	}
	rev = result.Batch.Revision
	seal := application.SealRequest{CommandMeta: application.CommandMeta{RequestID: "self-seal", ExpectedRevision: rev, Actor: "curator-c"}}
	if err := c.post(ctx, httpapi.URL(c.base, create.ID, "/archive"), seal, &result); err != nil {
		return err
	}
	if result.Manifest == nil {
		return fmt.Errorf("自检未返回封存清单")
	}
	var verification application.ManifestVerification
	if err := c.get(ctx, httpapi.URL(c.base, create.ID, "/manifest/verify?manifest_hash="+result.Manifest.ManifestHash), &verification); err != nil {
		return err
	}
	if !verification.Valid {
		return fmt.Errorf("封存清单核验失败")
	}
	var auditPage application.AuditPage
	if err := c.get(ctx, httpapi.URL(c.base, create.ID, "/audit-events?limit=50"), &auditPage); err != nil {
		return err
	}
	if len(auditPage.Events) != 7 || auditPage.HeadHash == "" {
		return fmt.Errorf("审计事件链不完整: %d", len(auditPage.Events))
	}
	return nil
}

func (c selfCheckClient) post(ctx context.Context, path string, body any, target any) error {
	if path[0] == '/' {
		path = c.base + path
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	return c.do(request, target)
}
func (c selfCheckClient) get(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return c.do(request, target)
}
func (c selfCheckClient) do(request *http.Request, target any) error {
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s %s 返回 %d: %s", request.Method, request.URL.Path, response.StatusCode, string(raw))
	}
	if err = json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("解码自检响应: %w", err)
	}
	return nil
}
