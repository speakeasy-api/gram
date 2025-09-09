package oops

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

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
		var payload any
		var se *ShareableError
		if errors.As(err, &se) {
			code = se.HTTPStatus()
			payload = se.AsGoa()
		} else {
			gerr := goa.NewServiceError(
				errors.New(CodeUnexpected.UserMessage()),
				string(CodeUnexpected),
				false,
				false,
				true,
			)
			logger.ErrorContext(r.Context(), "unexpected error", attr.SlogErrorID(gerr.ID), attr.SlogError(err))
			payload = gerr
		}

		w.WriteHeader(code)
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(payload)
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to encode response", attr.SlogError(err))
		}
	})
}
