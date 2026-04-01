package oops

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// ErrHandle returns a middleware that wraps an http handler. It calls the
// underlying handler and captures any returned error. The error is
// appropriately serialized back to the client. It recognizes ShareableError and
// goa.ServiceError types. Any other error types are treated as internal server
// errors and a generic message is returned to the client. All errors are logged
// with the provided logger.
func ErrHandle(logger *slog.Logger, handler func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := handler(w, r)
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
		switch {
		case errors.As(err, &se):
			code = se.HTTPStatus()
			payload = se.AsGoa()
		default:
			stack := string(debug.Stack())
			logger.ErrorContext(r.Context(), "unexpected error", attr.SlogErrorID(payload.ID), attr.SlogError(err), attr.SlogErrorStack(stack))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		err = json.NewEncoder(w).Encode(payload)
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to encode response", attr.SlogError(err))
		}
	})
}
