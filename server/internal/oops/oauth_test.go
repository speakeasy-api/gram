package oops

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
)

func TestOAuthErrHandleMappings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"unexpected", errors.New("private database details"), http.StatusInternalServerError, oautherr.CodeServerError},
		{"unavailable", fmt.Errorf("operation: %w", E(CodeUnavailable, errors.New("private database details"), "private service details")), http.StatusServiceUnavailable, oautherr.CodeTemporarilyUnavailable},
		{"unclassified unauthorized", C(CodeUnauthorized), http.StatusInternalServerError, oautherr.CodeServerError},
		{"unclassified not found", C(CodeNotFound), http.StatusInternalServerError, oautherr.CodeServerError},
		{"invalid grant", NewOAuthError(http.StatusBadRequest, oautherr.CodeInvalidGrant, "authorization expired", errors.New("private grant details")), http.StatusBadRequest, oautherr.CodeInvalidGrant},
		{"unserved endpoint", NewOAuthError(http.StatusNotFound, oautherr.CodeInvalidRequest, "authorization endpoint not found", errors.New("private issuer details")), http.StatusNotFound, oautherr.CodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logger, _ := captureLogger()
			response := httptest.NewRecorder()
			OAuthErrHandle(logger, func(w http.ResponseWriter, _ *http.Request) error {
				w.Header().Set("Retry-After", "5")
				return tc.err
			}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/token", nil))
			require.Equal(t, tc.status, response.Code)
			require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.Equal(t, "no-cache", response.Header().Get("Pragma"))
			require.Equal(t, "5", response.Header().Get("Retry-After"))
			var body oautherr.RFC6749Error
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, tc.code, body.Code)
			require.NotContains(t, response.Body.String(), "private")
		})
	}
}

func TestOAuthErrHandlePreservesSuccessfulRevocation(t *testing.T) {
	t.Parallel()
	logger, logs := captureLogger()
	response := httptest.NewRecorder()
	OAuthErrHandle(logger, func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/revoke", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Empty(t, response.Body.String())
	require.Empty(t, logs.String())
}

func TestOAuthErrHandleDoesNotAppendToCommittedResponse(t *testing.T) {
	t.Parallel()
	for _, explicitHeader := range []bool{false, true} {
		t.Run(fmt.Sprint(explicitHeader), func(t *testing.T) {
			t.Parallel()
			logger, _ := captureLogger()
			response := httptest.NewRecorder()
			OAuthErrHandle(logger, func(w http.ResponseWriter, _ *http.Request) error {
				if explicitHeader {
					w.WriteHeader(http.StatusBadRequest)
				}
				if _, err := w.Write([]byte(`{"error":"invalid_grant"}`)); err != nil {
					return fmt.Errorf("write test response: %w", err)
				}
				return errors.New("late error")
			}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/token", nil))
			require.JSONEq(t, `{"error":"invalid_grant"}`, response.Body.String())
		})
	}
}

type failingOAuthResponseWriter struct {
	*httptest.ResponseRecorder
	writes int
}

func (w *failingOAuthResponseWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("connection closed")
}

func TestOAuthErrHandleDoesNotRetryFailedBodyWrite(t *testing.T) {
	t.Parallel()
	logger, _ := captureLogger()
	response := &failingOAuthResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	OAuthErrHandle(logger, func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"access_token":"test"}`))
		if err != nil {
			return fmt.Errorf("write token response: %w", err)
		}
		return nil
	}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/token", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, response.writes)
}

func TestOAuthErrorPreservesCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("revoked sign-in")
	err := NewOAuthError(http.StatusBadRequest, oautherr.CodeInvalidGrant, "authorization expired", cause)
	require.ErrorIs(t, err, cause)
}
