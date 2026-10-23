package oops

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
)

// OAuthError is an explicitly classified token or revocation endpoint failure.
// Callers choose grant and client semantics at the operation that failed;
// generic unauthorized and not-found errors cannot identify a dead grant.
type OAuthError struct {
	// Status is the HTTP response status.
	Status int

	// Response is the public OAuth error body.
	Response oautherr.RFC6749Error

	// Cause is the internal failure, excluded from the response.
	Cause error
}

// NewOAuthError pairs an explicit OAuth response with its internal cause.
func NewOAuthError(status int, code, description string, cause error) *OAuthError {
	return &OAuthError{
		Status:   status,
		Response: oautherr.RFC6749Error{Code: code, Description: description, URI: ""},
		Cause:    cause,
	}
}

func (e *OAuthError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Response.Error(), e.Cause)
	}
	return e.Response.Error()
}

func (e *OAuthError) Unwrap() error { return e.Cause }

// OAuthErrHandle serializes errors returned by token and revocation handlers.
// It is not an authorization redirect or registration response policy.
// Unclassified failures preserve client credentials: temporarily_unavailable
// for outages and server_error otherwise. These authorization-endpoint codes
// are token-endpoint extensions that let clients retain credentials and retry.
func OAuthErrHandle(logger *slog.Logger, handler func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded := &oauthResponseWriter{ResponseWriter: w, committed: false}
		err := handler(recorded, r)
		if err == nil {
			return
		}

		status := http.StatusInternalServerError
		body := oautherr.RFC6749Error{Code: oautherr.CodeServerError, Description: "the request could not be completed", URI: ""}
		var classified *OAuthError
		var shareable *ShareableError
		switch {
		case errors.As(err, &classified):
			status, body = classified.Status, classified.Response
			logger.WarnContext(r.Context(), "oauth request rejected", attr.SlogOAuthError(body.Code), attr.SlogError(err))
		case errors.As(err, &shareable):
			if shareable.Code == CodeUnavailable {
				status = http.StatusServiceUnavailable
				body.Code = oautherr.CodeTemporarilyUnavailable
				body.Description = "the service is temporarily unavailable"
			}
			if !shareable.SpanHandled() {
				_ = shareable.LogError(r.Context(), logger)
			}
		default:
			logger.ErrorContext(r.Context(), "unexpected oauth error", attr.SlogError(err))
		}
		// A failed body write may return an error after the response began.
		// Logging is still useful, but appending another JSON body corrupts it.
		if recorded.committed {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(body); err != nil {
			logger.ErrorContext(r.Context(), "write oauth error response", attr.SlogError(err))
		}
	})
}

// oauthResponseWriter tracks final headers without buffering token responses.
type oauthResponseWriter struct {
	// ResponseWriter receives the original response without buffering.
	http.ResponseWriter

	// committed records whether final headers or a body have been written.
	committed bool
}

func (w *oauthResponseWriter) WriteHeader(status int) {
	if status >= http.StatusOK || status == http.StatusSwitchingProtocols {
		w.committed = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *oauthResponseWriter) Write(body []byte) (int, error) {
	w.committed = true
	n, err := w.ResponseWriter.Write(body)
	if err != nil {
		return n, fmt.Errorf("write OAuth response: %w", err)
	}
	return n, nil
}

func (w *oauthResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
