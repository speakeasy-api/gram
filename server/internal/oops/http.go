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
			clientName := ""
			var recovery *oauthui.RecoveryLink
			if presentation, ok := errors.AsType[*browserError](err); ok {
				reason = presentation.reason
				clientName = presentation.clientName
				recovery = presentation.recovery
			}
			page := oauthui.AuthorizationError{
				Recovery:       recovery,
				Setup:          false,
				Status:         code,
				Reason:         reason,
				Approval:       false,
				ErrorID:        payload.ID,
				Code:           payload.Name,
				Description:    payload.Message,
				CallbackURL:    "",
				ClientName:     clientName,
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

	// clientName is a display name from validated flow state.
	clientName string

	// recovery is a caller-controlled destination for restarting the flow.
	recovery *oauthui.RecoveryLink
}

func (e *browserError) Error() string { return e.cause.Error() }
func (e *browserError) Unwrap() error { return e.cause }

// WithBrowserReason annotates an error for browser routes. JSON wrappers still
// find the underlying ShareableError through errors.As.
func WithBrowserReason(err error, reason oauthui.Reason) error {
	if err == nil {
		return nil
	}
	presentation := browserPresentation(err)
	presentation.reason = reason
	return presentation
}

// WithBrowserClient identifies the client to restart from without adding a redirect.
// Callers must use a fixed label or a name from validated flow state.
func WithBrowserClient(err error, name string) error {
	if err == nil {
		return nil
	}
	presentation := browserPresentation(err)
	presentation.clientName = name
	return presentation
}

// WithBrowserRecovery adds an explicit recovery action to browser failures.
// Callers must use a trusted destination, never a callback or request URL.
func WithBrowserRecovery(err error, recovery *oauthui.RecoveryLink) error {
	if err == nil {
		return nil
	}
	presentation := browserPresentation(err)
	presentation.recovery = recovery
	return presentation
}

func browserPresentation(err error) *browserError {
	presentation := &browserError{cause: err, reason: oauthui.ReasonDefault, clientName: "", recovery: nil}
	if existing, ok := errors.AsType[*browserError](err); ok {
		*presentation = *existing
		presentation.cause = err
	}
	return presentation
}
