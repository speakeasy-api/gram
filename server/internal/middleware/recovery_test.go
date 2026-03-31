package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

type recoveryLogEntry struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Error string `json:"error.message"`
	Stack string `json:"error.stack"`
}

func captureRecoveryLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler), &buf
}

func parseRecoveryLogEntries(t *testing.T, buf *bytes.Buffer) []recoveryLogEntry {
	t.Helper()

	var entries []recoveryLogEntry
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}

		var entry recoveryLogEntry
		err := json.Unmarshal([]byte(line), &entry)
		require.NoError(t, err, "failed to parse log line: %s", line)
		entries = append(entries, entry)
	}

	return entries
}

func TestRecovery_PanicReturnsUnexpectedError(t *testing.T) {
	t.Parallel()

	logger, logBuf := captureRecoveryLogger()
	handler := NewRecovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "close", rec.Header().Get("Connection"))
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var response goa.ServiceError
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, string(oops.CodeUnexpected), response.Name)
	require.Equal(t, oops.CodeUnexpected.UserMessage(), response.Message)
	require.True(t, response.Fault)

	entries := parseRecoveryLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "ERROR", entries[0].Level)
	require.Equal(t, "recovered from panic", entries[0].Msg)
	require.Contains(t, entries[0].Error, "panic: boom")
	require.Contains(t, entries[0].Stack, "TestRecovery_PanicReturnsUnexpectedError")
}

func TestRecovery_PanicReturnsShareableError(t *testing.T) {
	t.Parallel()

	logger, _ := captureRecoveryLogger()
	panicErr := oops.E(oops.CodeBadRequest, nil, "invalid input")
	handler := NewRecovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(panicErr)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "close", rec.Header().Get("Connection"))
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var response goa.ServiceError
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, string(oops.CodeBadRequest), response.Name)
	require.Equal(t, "invalid input", response.Message)
	require.False(t, response.Fault)
	require.NotEmpty(t, response.ID)
}

func TestRecovery_PanicSkipsWritingUpgradeResponse(t *testing.T) {
	t.Parallel()

	logger, logBuf := captureRecoveryLogger()
	handler := NewRecovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Connection", "keep-alive, Upgrade")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Body.String())
	require.Empty(t, rec.Header().Get("Content-Type"))
	require.Empty(t, rec.Header().Get("Connection"))

	entries := parseRecoveryLogEntries(t, logBuf)
	require.Len(t, entries, 1)
	require.Equal(t, "recovered from panic", entries[0].Msg)
}

func TestRecovery_RepanicsAbortHandler(t *testing.T) {
	t.Parallel()

	logger, logBuf := captureRecoveryLogger()
	handler := NewRecovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	require.PanicsWithValue(t, http.ErrAbortHandler, func() {
		handler.ServeHTTP(rec, req)
	})
	require.Empty(t, logBuf.String())
}

func TestRecovery_RepanicsWrappedAbortHandler(t *testing.T) {
	t.Parallel()

	logger, logBuf := captureRecoveryLogger()
	panicErr := errors.Join(errors.New("wrapper"), http.ErrAbortHandler)
	handler := NewRecovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(panicErr)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	require.PanicsWithValue(t, panicErr, func() {
		handler.ServeHTTP(rec, req)
	})
	require.Empty(t, logBuf.String())
}
