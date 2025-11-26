package oops

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type panicError struct {
	cause error
}

func (e *panicError) Error() string {
	return fmt.Sprintf("%v", e.cause)
}

func (e *panicError) Unwrap() error {
	return e.cause
}

func handleWithRecovery(
	handler func(http.ResponseWriter, *http.Request) error,
	w http.ResponseWriter,
	r *http.Request,
) (recErr error) {
	defer func() {
		if rec := recover(); rec != nil {
			if err, ok := rec.(error); ok {
				recErr = &panicError{cause: err}
			} else {
				recErr = &panicError{cause: fmt.Errorf("panic: %v", rec)}
			}
		}
	}()

	return handler(w, r)
}

// ErrHandle returns a middleware that wraps an http handler. It calls the
// underlying handler and captures any returned error. The error is
// appropriately serialized back to the client. It recognizes ShareableError and
// goa.ServiceError types. Any other error types are treated as internal server
// errors and a generic message is returned to the client. All errors are logged
// with the provided logger.
func ErrHandle(logger *slog.Logger, handler func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := handleWithRecovery(handler, w, r)
		if err == nil {
			return
		}

		code := http.StatusInternalServerError
		payload := goa.NewServiceError(
			errors.New(CodeUnexpected.UserMessage()),
			string(CodeUnexpected),
			false,
			false,
			true,
		)

		var se *ShareableError
		var pe *panicError
		switch {
		case errors.As(err, &pe):
			stack := string(debug.Stack())
			logger.ErrorContext(r.Context(), "panic recovered in http handler", attr.SlogErrorID(payload.ID), attr.SlogError(pe), attr.SlogExceptionStacktrace(stack))
			w.Header().Set("Connection", "close")
		case errors.As(err, &se):
			code = se.HTTPStatus()
			payload = se.AsGoa()
		default:
			stack := string(debug.Stack())
			logger.ErrorContext(r.Context(), "unexpected error", attr.SlogErrorID(payload.ID), attr.SlogError(err), attr.SlogExceptionStacktrace(stack))
		}

		w.WriteHeader(code)
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(payload)
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to encode response", attr.SlogError(err))
		}
	})
}
