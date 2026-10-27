package oops

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
)

// ErrHandle returns a middleware that wraps an http handler. It calls the
// underlying handler and captures any returned error. The error is
// appropriately serialized back to the client. It recognizes ShareableError and
// goa.ServiceError types. Any other error types are treated as internal server
// errors and a generic message is returned to the client. All errors are logged
// with the provided logger.
func ErrHandle(logger *slog.Logger, handler func(http.ResponseWriter, *http.Request) error) http.Handler {
	return errHandle(logger, handler, false)
}

// BrowserErrHandle opts a navigation route into HTML failures. Machine endpoints
// keep ErrHandle or OAuthErrHandle; request headers never choose the format.
func BrowserErrHandle(logger *slog.Logger, handler func(http.ResponseWriter, *http.Request) error) http.Handler {
	return errHandle(logger, handler, true)
}

func errHandle(logger *slog.Logger, handler func(http.ResponseWriter, *http.Request) error, browser bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded := &oauthResponseWriter{ResponseWriter: w, committed: false}
		writer := w
		if browser {
			writer = recorded
		}
		err := handler(writer, r)
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
			code = se.HTTPStatus(r.Context())
			payload = se.AsGoa(r.Context())
		default:
			stack := string(debug.Stack())
			logger.ErrorContext(r.Context(), "unexpected error", attr.SlogErrorID(payload.ID), attr.SlogError(err), attr.SlogErrorStack(stack))
		}

		if recorded.committed {
			return
		}
		if browser {
			reason := oauthui.ReasonDefault
			if presentation, ok := errors.AsType[*browserError](err); ok {
				reason = presentation.reason
			}
			page := oauthui.AuthorizationError{
				Status:         code,
				Reason:         reason,
				Approval:       false,
				ErrorID:        payload.ID,
				Code:           payload.Name,
				Description:    payload.Message,
				CallbackURL:    "",
				ClientName:     "",
				ClientID:       "",
				ClientIDOrigin: "",
				ServerName:     "",
			}
			if err := oauthui.WriteAuthorizationError(r.Context(), w, page); err != nil {
				logger.ErrorContext(r.Context(), "write browser error response", attr.SlogError(err))
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		err = json.NewEncoder(w).Encode(payload)
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to encode response", attr.SlogError(err))
		}
	})
}

// browserError carries presentation metadata without changing the underlying
// error's public code, status, identifier, or machine serialization.
type browserError struct {
	// cause is the error whose transport semantics remain authoritative.
	cause error

	// reason selects actionable browser copy.
	reason oauthui.Reason
}

func (e *browserError) Error() string { return e.cause.Error() }
func (e *browserError) Unwrap() error { return e.cause }

// WithBrowserReason annotates an error for browser routes. JSON wrappers still
// find the underlying ShareableError through errors.As.
func WithBrowserReason(err error, reason oauthui.Reason) error {
	if err == nil {
		return nil
	}
	return &browserError{cause: err, reason: reason}
}
