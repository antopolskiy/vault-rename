package apperr

import "fmt"

const (
	CodeInvalidName          = "INVALID_NAME"
	CodeNoChange             = "NO_CHANGE"
	CodeSourceNotFound       = "SOURCE_NOT_FOUND"
	CodeSourceOutsideVault   = "SOURCE_OUTSIDE_VAULT"
	CodeTargetExists         = "TARGET_EXISTS"
	CodeAmbiguousReference   = "AMBIGUOUS_REFERENCE"
	CodeUnsupportedReference = "UNSUPPORTED_REFERENCE"
	CodeReferencesPresent    = "REFERENCES_PRESENT"
	CodeSourceChanged        = "SOURCE_CHANGED"
	CodeVaultBusy            = "VAULT_BUSY"
	CodeRecoveryConflict     = "RECOVERY_CONFLICT"
	CodeRollbackFailed       = "ROLLBACK_FAILED"
	CodeConfigError          = "CONFIG_ERROR"
	CodeIOError              = "IO_ERROR"
)

type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	Err     error          `json:"-"`
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func New(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Wrap(code, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}

func WithDetails(err *Error, details map[string]any) *Error {
	err.Details = details
	return err
}
