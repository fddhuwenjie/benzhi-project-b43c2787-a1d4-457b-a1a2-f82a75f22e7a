package httpapi

import (
	"encoding/json"
	"errors"

	"io"
	"net/http"

	"strings"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/domain"
)

func requestHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func decode(w http.ResponseWriter, r *http.Request, target any) error {
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		return domain.NewError(domain.CodeValidation, "Content-Type 必须为 application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return domain.NewError(domain.CodeValidation, "JSON 请求无效: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return domain.NewError(domain.CodeValidation, "请求体只能包含一个 JSON 对象")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type problem struct {
	Type                 string           `json:"type"`
	Title                string           `json:"title"`
	Status               int              `json:"status"`
	Code                 domain.ErrorCode `json:"code"`
	Detail               string           `json:"detail"`
	CurrentRevision      int64            `json:"current_revision,omitempty"`
	Verified             *bool            `json:"verified,omitempty"`
	FirstInvalidSequence int64            `json:"first_invalid_sequence,omitempty"`
	IntegrityDetail      string           `json:"integrity_detail,omitempty"`
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := domain.ErrorCode("internal_error")
	title := "服务内部错误"
	detail := "请求处理失败"
	var de *domain.Error
	if errors.As(err, &de) {
		code = de.Code
		detail = de.Message
		title = "业务请求失败"
		switch de.Code {
		case domain.CodeValidation:
			status = 400
		case domain.CodeNotFound:
			status = 404
		case domain.CodeNotCalibrated, domain.CodeNotEvaluated:
			status = 404
		case domain.CodeConflict, domain.CodeDuplicate, domain.CodeIdempotency, domain.CodeInvalidState, domain.CodeSealed:
			status = 409
		case domain.CodeForbidden:
			status = 403
		case domain.CodeIntegrity:
			status = 500
		}
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Type: "urn:astroplate-vault:problem:" + string(code), Title: title, Status: status, Code: code, Detail: detail, CurrentRevision: func() int64 {
		if de != nil {
			return de.CurrentRevision
		}
		return 0
	}()})
}

func command(w http.ResponseWriter, r *http.Request, target any, execute func() (application.CommandResult, error), status int) {
	if err := decode(w, r, target); err != nil {
		writeError(w, err)
		return
	}
	result, err := execute()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, status, result)
}

func (a *API) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

