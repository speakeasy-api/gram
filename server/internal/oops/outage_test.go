package oops_test

import (
	"context"
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
		{name: "server shutting down", err: fmt.Errorf("query: %w", &pgconn.PgError{Code: "57P01"}), want: true},
		{name: "crash shutdown", err: &pgconn.PgError{Code: "57P02"}, want: true},
		{name: "starting up", err: &pgconn.PgError{Code: "57P03"}, want: true},
		{name: "connection exception", err: &pgconn.PgError{Code: "08006"}, want: true},
		{name: "too many connections", err: &pgconn.PgError{Code: "53300"}, want: true},
		{name: "serialization failure", err: &pgconn.PgError{Code: "40001"}, want: true},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: true},
		{name: "pool wait ran out", err: fmt.Errorf("acquire: %w", context.DeadlineExceeded), want: true},
		{name: "syntax error", err: &pgconn.PgError{Code: "42601"}, want: false},
		{name: "missing table", err: &pgconn.PgError{Code: "42P01"}, want: false},
		{name: "missing column", err: &pgconn.PgError{Code: "42703"}, want: false},
		{name: "insufficient privilege", err: &pgconn.PgError{Code: "42501"}, want: false},
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

func TestIsStoreFailure(t *testing.T) {
	t.Parallel()

	require.True(t, oops.IsStoreFailure(fmt.Errorf("load: %w", &pgconn.PgError{Code: "42P01"})), "a broken deploy")
	require.True(t, oops.IsStoreFailure(&pgconn.PgError{Code: "57P01"}), "an outage")
	require.False(t, oops.IsStoreFailure(fmt.Errorf("load: %w", pgx.ErrNoRows)), "a missing row is an answer")
	require.False(t, oops.IsStoreFailure(oops.C(oops.CodeUnauthorized)), "a refusal")
}

func TestErrHandlersAnswerInfrastructureFailureAsOutage(t *testing.T) {
	t.Parallel()

	down := oops.E(oops.CodeUnexpected, &pgconn.PgError{Code: "57P01"}, "load mcp endpoint")
	failing := func(http.ResponseWriter, *http.Request) error { return down }
	for name, handler := range map[string]http.Handler{
		"resource": oops.MCPErrHandle(testenv.NewLogger(t), failing),
		"token":    oops.OAuthErrHandle(testenv.NewLogger(t), failing),
		"machine":  oops.AuthErrHandle(testenv.NewLogger(t), failing),
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

// Chat, webhooks, instance execution and admin keep their own retry contract.
func TestErrHandleLeavesStoreFailuresAlone(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	oops.ErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error {
		return oops.E(oops.CodeUnexpected, &pgconn.PgError{Code: "57P01"}, "load")
	}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Empty(t, w.Header().Get("Retry-After"))
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
		{name: "broken query", err: oops.E(oops.CodeUnexpected, &pgconn.PgError{Code: "42601"}, "bug"), want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			oops.AuthErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error { return tc.err }).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			require.Equal(t, tc.want, w.Code)
			require.Empty(t, w.Header().Get("Retry-After"))
		})
	}
}
