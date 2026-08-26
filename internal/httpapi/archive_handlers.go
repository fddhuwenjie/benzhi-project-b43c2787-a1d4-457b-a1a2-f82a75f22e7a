package httpapi

import (
	"encoding/json"

	"net/http"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/domain"
)

func (a *API) GetBatch(w http.ResponseWriter, r *http.Request) {
	batch, err := a.service.GetProjection(r.Context(), r.PathValue("batchID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, batch)
}
func (a *API) GetManifest(w http.ResponseWriter, r *http.Request) {
	manifest, err := a.service.GetManifest(r.Context(), r.PathValue("batchID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, manifest)
}
func (a *API) VerifyManifest(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.VerifyManifest(r.Context(), r.PathValue("batchID"), r.URL.Query().Get("manifest_hash"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (a *API) ReconcileManifest(w http.ResponseWriter, r *http.Request) {
	var request application.ManifestReconcileRequest
	if err := decode(w, r, &request); err != nil {
		writeError(w, err)
		return
	}
	result, err := a.service.ReconcileManifest(r.Context(), r.PathValue("batchID"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (a *API) GetQualityPreview(w http.ResponseWriter, r *http.Request) {
	expected, err := requiredRevision(r)
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := a.service.QualityPreview(r.Context(), r.PathValue("batchID"), expected)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (a *API) GetPeerReviewWorkItem(w http.ResponseWriter, r *http.Request) {
	expected, err := requiredRevision(r)
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := a.service.PeerReviewWorkItem(r.Context(), r.PathValue("batchID"), r.URL.Query().Get("reviewer"), expected)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (a *API) ListAuditEvents(w http.ResponseWriter, r *http.Request) {
	after, err := queryInt(r, "after", 0)
	if err != nil {
		writeError(w, err)
		return
	}
	limit, err := queryInt(r, "limit", 50)
	if err != nil {
		writeError(w, err)
		return
	}
	if after < 0 || limit < 1 || limit > 200 {
		writeError(w, domain.NewError(domain.CodeValidation, "after 必须非负，limit 必须在 1 到 200 之间"))
		return
	}
	before, err := queryInt(r, "before", 0)
	if err != nil {
		writeError(w, err)
		return
	}
	if before < 0 {
		writeError(w, domain.NewError(domain.CodeValidation, "before 必须非负"))
		return
	}
	if before > 0 && before <= after {
		writeError(w, domain.NewError(domain.CodeValidation, "before 必须大于 after"))
		return
	}
	revisionFrom, err := queryInt64(r, "revision_from", 0)
	if err != nil {
		writeError(w, err)
		return
	}
	revisionTo, err := queryInt64(r, "revision_to", 0)
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := a.service.SearchAudit(r.Context(), r.PathValue("batchID"), application.AuditFilter{EventType: r.URL.Query().Get("event_type"), Actor: r.URL.Query().Get("actor"), RequestID: r.URL.Query().Get("request_id"), RevisionFrom: revisionFrom, RevisionTo: revisionTo, After: int64(after), Before: int64(before), Limit: limit})
	if err != nil {
		if page.IntegrityDetail != "" {
			verified := false
			w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
			w.WriteHeader(500)
			_ = json.NewEncoder(w).Encode(problem{Type: "urn:astroplate-vault:problem:integrity_error", Title: "审计链完整性校验失败", Status: 500, Code: domain.CodeIntegrity, Detail: page.IntegrityDetail, Verified: &verified, FirstInvalidSequence: page.FirstInvalidSequence, IntegrityDetail: page.IntegrityDetail})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, 200, page)
}

