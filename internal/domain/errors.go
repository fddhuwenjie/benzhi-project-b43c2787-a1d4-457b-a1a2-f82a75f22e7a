package domain

import "fmt"

type ErrorCode string

const (
	CodeValidation    ErrorCode = "validation_error"
	CodeNotFound      ErrorCode = "not_found"
	CodeConflict      ErrorCode = "revision_conflict"
	CodeInvalidState  ErrorCode = "invalid_state"
	CodeDuplicate     ErrorCode = "duplicate"
	CodeForbidden     ErrorCode = "forbidden"
	CodeSealed        ErrorCode = "batch_sealed"
	CodeIdempotency   ErrorCode = "idempotency_conflict"
	CodeIntegrity     ErrorCode = "integrity_error"
	CodeNotCalibrated ErrorCode = "not_calibrated"
	CodeNotEvaluated  ErrorCode = "not_evaluated"
)

type Error struct {
	Code            ErrorCode `json:"code"`
	Message         string    `json:"message"`
	CurrentRevision int64     `json:"current_revision,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func NewError(code ErrorCode, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func RevisionConflict(current int64) error {
	return &Error{Code: CodeConflict, Message: "expected_revision 与当前修订不一致", CurrentRevision: current}
}
