package httpapi

import (
	"fmt"

	"net/http"
	"strconv"
	"strings"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/domain"
)

func (a *API) GetCalibration(w http.ResponseWriter, r *http.Request) {
	v, e := a.service.CalibrationHistory(r.Context(), r.PathValue("batchID"), r.URL.Query().Get("result"))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (a *API) GetScanProgress(w http.ResponseWriter, r *http.Request) {
	start, e := queryInt(r, "catalog_start", 0)
	if e != nil {
		writeError(w, e)
		return
	}
	end, e := queryInt(r, "catalog_end", 0)
	if e != nil {
		writeError(w, e)
		return
	}
	after, e := queryInt(r, "after_catalog", 0)
	if e != nil {
		writeError(w, e)
		return
	}
	limit, e := queryInt(r, "limit", 100)
	if e != nil {
		writeError(w, e)
		return
	}
	if r.URL.Query().Get("catalog_start") != "" && start < 1 || r.URL.Query().Get("catalog_end") != "" && end < 1 || limit < 1 {
		writeError(w, domain.NewError(domain.CodeValidation, "目录范围必须为正整数，limit 必须为正整数"))
		return
	}
	v, e := a.service.ScanProgress(r.Context(), r.PathValue("batchID"), application.ScanProgressQuery{CatalogStart: start, CatalogEnd: end, Status: r.URL.Query().Get("status"), AfterCatalog: after, Limit: limit})
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}

func (a *API) GetArchiveReadiness(w http.ResponseWriter, r *http.Request) {
	expected, e := queryInt64(r, "expected_revision", 0)
	if e != nil {
		writeError(w, e)
		return
	}
	v, e := a.service.ArchiveReadiness(r.Context(), r.PathValue("batchID"), expected)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (a *API) GetQualityResult(w http.ResponseWriter, r *http.Request) {
	v, e := a.service.QualityResult(r.Context(), r.PathValue("batchID"), r.URL.Query().Get("rule_code"), r.URL.Query().Get("passed"), r.URL.Query().Get("catalog_number"))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (a *API) GetIssueQueue(w http.ResponseWriter, r *http.Request) {
	limit, e := queryInt(r, "limit", 50)
	if e != nil {
		writeError(w, e)
		return
	}
	v, e := a.service.IssueQueue(r.Context(), r.PathValue("batchID"), r.URL.Query().Get("status"), r.URL.Query().Get("severity"), r.URL.Query().Get("rule_code"), r.URL.Query().Get("after"), limit)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (a *API) GetPeerHistory(w http.ResponseWriter, r *http.Request) {
	v, e := a.service.PeerReviewHistory(r.Context(), r.PathValue("batchID"), r.URL.Query().Get("reviewer"))
	if e != nil {
		writeError(w, e)
		return
	}
	after, _ := queryInt(r, "after", 0)
	limit, _ := queryInt(r, "limit", 50)
	if after < 0 || limit < 1 || limit > 200 {
		writeError(w, domain.NewError(domain.CodeValidation, "分页参数无效"))
		return
	}
	if after < int(len(v.Rounds)) {
		v.Rounds = v.Rounds[after:]
	}
	if len(v.Rounds) > limit {
		v.Rounds = v.Rounds[:limit]
	}
	writeJSON(w, 200, v)
}
func (a *API) GetManifestPreview(w http.ResponseWriter, r *http.Request) {
	exp, e := queryInt64(r, "expected_revision", 0)
	if e != nil {
		writeError(w, e)
		return
	}
	v, e := a.service.ManifestPreview(r.Context(), r.PathValue("batchID"), exp)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}

func queryInt64(r *http.Request, name string, fallback int64) (int64, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback, nil
	}
	n, e := strconv.ParseInt(v, 10, 64)
	if e != nil {
		return 0, domain.NewError(domain.CodeValidation, "查询参数 %s 必须为整数", name)
	}
	return n, nil
}

func requiredRevision(r *http.Request) (int64, error) {
	if r.URL.Query().Get("expected_revision") == "" {
		return 0, domain.NewError(domain.CodeValidation, "缺少 expected_revision 查询参数")
	}
	n, err := queryInt64(r, "expected_revision", 0)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, domain.NewError(domain.CodeValidation, "expected_revision 必须为正整数")
	}
	return n, nil
}

func queryInt(r *http.Request, name string, fallback int) (int, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, domain.NewError(domain.CodeValidation, "查询参数 %s 必须为整数", name)
	}
	return n, nil
}

func URL(base, batchID, suffix string) string {
	return fmt.Sprintf("%s/api/v1/plate-batches/%s%s", strings.TrimRight(base, "/"), batchID, suffix)
}

