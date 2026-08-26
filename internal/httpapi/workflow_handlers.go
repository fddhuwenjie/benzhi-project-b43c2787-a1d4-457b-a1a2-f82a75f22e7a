package httpapi

import (
	"net/http"
	"strconv"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/domain"
)

func (a *API) EvaluateQuality(w http.ResponseWriter, r *http.Request) {
	var request application.EvaluateRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.Evaluate(r.Context(), r.PathValue("batchID"), request)
	}, 200)
}
func (a *API) ResolveIssue(w http.ResponseWriter, r *http.Request) {
	var request application.ResolveIssueRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.ResolveIssue(r.Context(), r.PathValue("batchID"), r.PathValue("issueID"), request)
	}, 200)
}
func (a *API) RevokeIssueResolution(w http.ResponseWriter, r *http.Request) {
	var request application.ResolutionRevocationRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.RevokeIssueResolution(r.Context(), r.PathValue("batchID"), r.PathValue("issueID"), request)
	}, 200)
}
func (a *API) ResolveIssuesBatch(w http.ResponseWriter, r *http.Request) {
	var request application.BatchIssueResolutionRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.ResolveIssuesBatch(r.Context(), r.PathValue("batchID"), request)
	}, 200)
}

func (a *API) ResolveByRescan(w http.ResponseWriter, r *http.Request) {
	var request application.RescanResolutionRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.ResolveByRescan(r.Context(), r.PathValue("batchID"), request)
	}, 200)
}
func (a *API) RequestPeerReview(w http.ResponseWriter, r *http.Request) {
	var request application.PeerReviewRequestRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.RequestPeerReview(r.Context(), r.PathValue("batchID"), request)
	}, 200)
}
func (a *API) SubmitPeerReview(w http.ResponseWriter, r *http.Request) {
	var request application.PeerReviewRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.SubmitPeerReview(r.Context(), r.PathValue("batchID"), request)
	}, 200)
}
func (a *API) SubmitPeerReviewEvidence(w http.ResponseWriter, r *http.Request) {
	var request application.PeerReviewEvidenceRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.SubmitPeerReviewEvidence(r.Context(), r.PathValue("batchID"), request)
	}, 200)
}

func (a *API) CreatePeerReviewDraft(w http.ResponseWriter, r *http.Request) {
	var request application.CreatePeerReviewDraftRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.CreatePeerReviewDraft(r.Context(), r.PathValue("batchID"), request)
	}, http.StatusCreated)
}

func (a *API) GetPeerReviewDraft(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.GetPeerReviewDraft(r.Context(), r.PathValue("batchID"), r.PathValue("draftID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (a *API) PutPeerReviewDraftEvidence(w http.ResponseWriter, r *http.Request) {
	var request application.PutPeerReviewDraftEvidenceRequest
	if err := decode(w, r, &request); err != nil {
		writeError(w, err)
		return
	}
	catalog, err := strconv.Atoi(r.PathValue("catalogNumber"))
	if err != nil || catalog < 1 {
		writeError(w, domain.NewError(domain.CodeValidation, "catalogNumber 必须为正整数"))
		return
	}
	if request.CatalogNumber != 0 && request.CatalogNumber != catalog {
		writeError(w, domain.NewError(domain.CodeValidation, "请求体 catalog_number 与路径不一致"))
		return
	}
	request.CatalogNumber = catalog
	result, err := a.service.PutPeerReviewDraftEvidence(r.Context(), r.PathValue("batchID"), r.PathValue("draftID"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (a *API) CompletePeerReviewDraft(w http.ResponseWriter, r *http.Request) {
	var request application.CompletePeerReviewDraftRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.CompletePeerReviewDraft(r.Context(), r.PathValue("batchID"), r.PathValue("draftID"), request)
	}, 200)
}
func (a *API) SealBatch(w http.ResponseWriter, r *http.Request) {
	var request application.SealRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.Seal(r.Context(), r.PathValue("batchID"), request)
	}, 200)
}

