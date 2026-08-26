package httpapi

import (
	"net/http"

	"astroplate-vault/internal/application"
)

const maxBodyBytes = 1 << 20

type API struct{ service *application.Service }

func New(service *application.Service) *API { return &API{service: service} }

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.Health)
	mux.HandleFunc("POST /api/v1/plate-batches", a.CreateBatch)
	mux.HandleFunc("GET /api/v1/plate-batches", a.ListBatches)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}", a.GetBatch)
	mux.HandleFunc("PATCH /api/v1/plate-batches/{batchID}", a.ReviseBatch)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/calibrations", a.SubmitCalibration)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/scans", a.RegisterScan)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/scans/{scanID}/corrections", a.CorrectScan)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/scans/batch", a.RegisterBatchScans)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/batch-scans", a.RegisterBatchScans)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/catalog-precheck", a.PrecheckCatalogs)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/catalogs/precheck", a.PrecheckCatalogs)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/quality-evaluations", a.EvaluateQuality)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/quality-evaluations/preview", a.GetQualityPreview)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/issues/{issueID}/resolution", a.ResolveIssue)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/issues/{issueID}/resolution-revocations", a.RevokeIssueResolution)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/issues/resolutions:batch", a.ResolveIssuesBatch)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/issues/rescan-resolution", a.ResolveByRescan)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/peer-review-request", a.RequestPeerReview)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/peer-reviews", a.SubmitPeerReview)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/peer-reviews/evidence", a.SubmitPeerReviewEvidence)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/peer-reviews/work-item", a.GetPeerReviewWorkItem)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/peer-reviews/drafts", a.CreatePeerReviewDraft)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/peer-reviews/drafts/{draftID}", a.GetPeerReviewDraft)
	mux.HandleFunc("PUT /api/v1/plate-batches/{batchID}/peer-reviews/drafts/{draftID}/evidence/{catalogNumber}", a.PutPeerReviewDraftEvidence)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/peer-reviews/drafts/{draftID}/completion", a.CompletePeerReviewDraft)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/archive", a.SealBatch)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/manifest", a.GetManifest)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/manifest/verify", a.VerifyManifest)
	mux.HandleFunc("POST /api/v1/plate-batches/{batchID}/manifest/reconcile", a.ReconcileManifest)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/audit-events", a.ListAuditEvents)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/calibrations", a.GetCalibration)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/calibration", a.GetCalibration)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/quality-results", a.GetQualityResult)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/quality-evaluations", a.GetQualityResult)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/issues", a.GetIssueQueue)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/peer-reviews/history", a.GetPeerHistory)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/archive/preview", a.GetManifestPreview)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/archive/readiness", a.GetArchiveReadiness)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/scans/progress", a.GetScanProgress)
	mux.HandleFunc("GET /api/v1/plate-batches/{batchID}/manifest/preview", a.GetManifestPreview)
	return requestHeaders(mux)
}

