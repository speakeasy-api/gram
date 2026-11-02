package oops_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestIsInfrastructureFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "missing table", err: &pgconn.PgError{Code: "42P01"}, want: true},
		{name: "server shutting down", err: fmt.Errorf("query: %w", &pgconn.PgError{Code: "57P01"}), want: true},
		{name: "unique violation", err: &pgconn.PgError{Code: "23505"}, want: false},
		{name: "bad data", err: &pgconn.PgError{Code: "22P02"}, want: false},
		{name: "no rows", err: fmt.Errorf("load: %w", pgx.ErrNoRows), want: false},
		{name: "other", err: errors.New("boom"), want: false},
		{name: "nil", err: nil, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, oops.IsInfrastructureFailure(tc.err))
		})
	}
}

func TestErrHandlersAnswerInfrastructureFailureAsOutage(t *testing.T) {
	t.Parallel()

	down := oops.E(oops.CodeUnexpected, &pgconn.PgError{Code: "57P01"}, "load mcp endpoint")
	failing := func(http.ResponseWriter, *http.Request) error { return down }
	for name, handler := range map[string]http.Handler{
		"resource": oops.MCPErrHandle(testenv.NewLogger(t), failing),
		"token":    oops.OAuthErrHandle(testenv.NewLogger(t), failing),
		"machine":  oops.ErrHandle(testenv.NewLogger(t), failing),
		"browser":  oops.BrowserErrHandle(testenv.NewLogger(t), failing),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
			require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
			require.Equal(t, "30", w.Header().Get("Retry-After"))
			require.Empty(t, w.Header().Get("WWW-Authenticate"))
			require.Empty(t, w.Header().Get(constants.HeaderProxiedResponse), "our own failure keeps alerting")
		})
	}

	token := httptest.NewRecorder()
	oops.OAuthErrHandle(testenv.NewLogger(t), failing).ServeHTTP(token, httptest.NewRequest(http.MethodPost, "/", nil))
	var body oautherr.RFC6749Error
	require.NoError(t, json.Unmarshal(token.Body.Bytes(), &body))
	require.Equal(t, oautherr.CodeTemporarilyUnavailable, body.Code)
}

func TestErrHandlersKeepAnswersThatAreNotOutages(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: oops.E(oops.CodeNotFound, pgx.ErrNoRows, "mcp endpoint not found"), want: http.StatusNotFound},
		{name: "unauthorized despite a store cause", err: oops.E(oops.CodeUnauthorized, &pgconn.PgError{Code: "57P01"}, "no"), want: http.StatusUnauthorized},
		{name: "unexpected missing row", err: oops.E(oops.CodeUnexpected, pgx.ErrNoRows, "gone"), want: http.StatusInternalServerError},
		{name: "unexpected bug", err: oops.E(oops.CodeUnexpected, errors.New("boom"), "bug"), want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			oops.ErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error { return tc.err }).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			require.Equal(t, tc.want, w.Code)
			require.Empty(t, w.Header().Get("Retry-After"))
		})
	}
}
