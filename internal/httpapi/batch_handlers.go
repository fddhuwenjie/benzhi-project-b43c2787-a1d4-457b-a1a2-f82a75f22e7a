package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/domain"
)

func (a *API) CreateBatch(w http.ResponseWriter, r *http.Request) {
	var request application.CreateBatchRequest
	command(w, r, &request, func() (application.CommandResult, error) { return a.service.CreateBatch(r.Context(), request) }, http.StatusCreated)
}

func queryValue(r *http.Request, name string) (string, error) {
	values, present := r.URL.Query()[name]
	if !present {
		return "", nil
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", domain.NewError(domain.CodeValidation, "%s 筛选值不能为空且不能重复", name)
	}
	return values[0], nil
}

func (a *API) ListBatches(w http.ResponseWriter, r *http.Request) {
	q := application.BatchWorkbenchQuery{Limit: 50}
	fields := []struct {
		name   string
		target *string
	}{{"state", &q.State}, {"scanner_id", &q.ScannerID}, {"quality_policy_version", &q.QualityPolicyVersion}, {"created_by", &q.CreatedBy}, {"title", &q.Title}, {"cursor", &q.Cursor}}
	for _, field := range fields {
		value, err := queryValue(r, field.name)
		if err != nil {
			writeError(w, err)
			return
		}
		*field.target = value
	}
	if raw, present := r.URL.Query()["limit"]; present {
		if len(raw) != 1 || strings.TrimSpace(raw[0]) == "" {
			writeError(w, domain.NewError(domain.CodeValidation, "limit 不能为空且不能重复"))
			return
		}
		var err error
		q.Limit, err = strconv.Atoi(raw[0])
		if err != nil {
			writeError(w, domain.NewError(domain.CodeValidation, "limit 必须为整数"))
			return
		}
	}
	result, err := a.service.BatchWorkbench(r.Context(), q)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (a *API) SubmitCalibration(w http.ResponseWriter, r *http.Request) {
	var request application.CalibrationRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.Calibrate(r.Context(), r.PathValue("batchID"), request)
	}, 200)
}
func (a *API) ReviseBatch(w http.ResponseWriter, r *http.Request) {
	var request application.ReviseBatchRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.ReviseBatch(r.Context(), r.PathValue("batchID"), request)
	}, 200)
}
func (a *API) RegisterScan(w http.ResponseWriter, r *http.Request) {
	var request application.ScanRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.AddScan(r.Context(), r.PathValue("batchID"), request)
	}, 200)
}
func (a *API) CorrectScan(w http.ResponseWriter, r *http.Request) {
	var request application.ScanCorrectionRequest
	command(w, r, &request, func() (application.CommandResult, error) {
		return a.service.CorrectScan(r.Context(), r.PathValue("batchID"), r.PathValue("scanID"), request)
	}, 200)
}
func (a *API) RegisterBatchScans(w http.ResponseWriter, r *http.Request) {
	var req application.BatchScanRequest
	command(w, r, &req, func() (application.CommandResult, error) {
		return a.service.BatchAddScans(r.Context(), r.PathValue("batchID"), req)
	}, 200)
}
func (a *API) PrecheckCatalogs(w http.ResponseWriter, r *http.Request) {
	var req application.CatalogPrecheckRequest
	commandPrecheck := func() (any, error) { return a.service.PrecheckCatalogs(r.Context(), r.PathValue("batchID"), req) }
	if err := decode(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	v, e := commandPrecheck()
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}

