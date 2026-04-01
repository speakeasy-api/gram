package oops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
)

type logEntry struct {
	Level   string         `json:"level"`
	Msg     string         `json:"msg"`
	Attrs   map[string]any `json:"attrs"`
	ErrorID string         `json:"error.id"`
	Error   string         `json:"error.message"`
	Stack   string         `json:"error.stack"`
}

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)
	return logger, &buf
}

func parseLogEntries(t *testing.T, buf *bytes.Buffer) []logEntry {
	t.Helper()
	var entries []logEntry
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var entry logEntry
		err := json.Unmarshal([]byte(line), &entry)
		require.NoError(t, err, "failed to parse log line: %s", line)
		entries = append(entries, entry)
	}
	return entries
}

func TestErrHandle_NoError(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()

	handler := ErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "success", rec.Body.String())
	require.Empty(t, logBuf.String(), "should not log anything on success")
}

func TestErrHandle_ShareableError(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()

	testErr := E(CodeNotFound, nil, "resource not found")

	handler := ErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		return testErr
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var response goa.ServiceError
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, string(CodeNotFound), response.Name)
	require.Equal(t, "resource not found", response.Message)

	require.Empty(t, logBuf.String(), "ShareableError should not log in ErrHandle")
}

func TestErrHandle_UnexpectedError(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()

	testErr := errors.New("database connection failed")

	handler := ErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		return testErr
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var response goa.ServiceError
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, string(CodeUnexpected), response.Name)
	require.NotEmpty(t, response.ID)

	entries := parseLogEntries(t, logBuf)
	require.Len(t, entries, 1)

	entry := entries[0]
	require.Equal(t, "ERROR", entry.Level)
	require.Equal(t, "unexpected error", entry.Msg)
	require.Contains(t, entry.Error, "database connection failed")
	require.NotEmpty(t, entry.Stack, "should include stack trace")
	require.Contains(t, entry.Stack, t.Name(), "stack trace should include test function")
	require.NotEmpty(t, entry.ErrorID)
}

func TestErrHandle_WrappedShareableError(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()

	innerErr := E(CodeBadRequest, nil, "invalid input")
	wrappedErr := errors.Join(errors.New("wrapper"), innerErr)

	handler := ErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		return wrappedErr
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)

	var response goa.ServiceError
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, string(CodeBadRequest), response.Name)
	require.Equal(t, "invalid input", response.Message)

	require.Empty(t, logBuf.String(), "ShareableError should not log even when wrapped")
}

type contextKey string

func TestErrHandle_ContextPropagation(t *testing.T) {
	t.Parallel()
	logger, logBuf := captureLogger()

	const ctxKey contextKey = "test-key"
	ctxValue := "test-value"

	var ctxValueReceived string
	handler := ErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		val := r.Context().Value(ctxKey)
		if v, ok := val.(string); ok {
			ctxValueReceived = v
		}
		return errors.New("test error")
	})

	ctx := context.WithValue(context.Background(), ctxKey, ctxValue)
	req := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotEmpty(t, logBuf.String())
	require.Equal(t, ctxValue, ctxValueReceived, "context should be propagated")
}
