package codex

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestListLogsSendsAuthAndFilters(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/organizations/org-123/logs" {
			t.Errorf("expected path /organizations/org-123/logs, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer codex-key" {
			t.Errorf("expected bearer auth, got %q", got)
		}
		if got := r.URL.Query().Get("event_type"); got != "COSTS" {
			t.Errorf("expected event_type COSTS, got %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "100" {
			t.Errorf("expected limit 100, got %q", got)
		}
		if got := r.URL.Query().Get("after"); got != "2026-07-16T00:00:00Z" {
			t.Errorf("expected after timestamp, got %q", got)
		}

		_, _ = w.Write([]byte(`{
			"data": [{
				"id": "eclf_123",
				"event_type": "COSTS",
				"end_time": "2026-07-16T01:00:00.123456Z",
				"file_name": "COSTS_2026-07-16T01:00:00.123456+00:00.jsonl",
				"file_size": 641,
				"file_sha256": "abc123"
			}],
			"has_more": true,
			"last_end_time": "2026-07-16T01:00:00.123456Z"
		}`))
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithAPIKey("codex-key"))
	page, err := client.ListLogs(t.Context(), ListLogsParams{
		PrincipalID: "org-123",
		EventType:   "COSTS",
		After:       time.Date(2026, 7, 16, 0, 0, 0, 123456789, time.UTC),
		Limit:       100,
	})
	require.NoError(t, err)
	require.True(t, page.HasMore)
	require.Equal(t, "eclf_123", page.Data[0].ID)
	require.Equal(t, int64(641), page.Data[0].FileSize)
	require.Equal(t, "abc123", page.Data[0].FileSHA256)
	require.Equal(t, time.Date(2026, 7, 16, 1, 0, 0, 123456000, time.UTC), page.LastEndTime)
}

func TestListLogsUsesWorkspaceSegmentForNonOrgPrincipal(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspaces/ws_123/logs" {
			t.Errorf("expected path /workspaces/ws_123/logs, got %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data": [], "has_more": false}`))
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithAPIKey("codex-key"))
	_, err := client.ListLogs(t.Context(), ListLogsParams{PrincipalID: "ws_123"})
	require.NoError(t, err)
}

func TestDownloadLogReturnsBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/organizations/org-123/logs/eclf_123" {
			t.Errorf("expected path /organizations/org-123/logs/eclf_123, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer codex-key" {
			t.Errorf("expected bearer auth, got %q", got)
		}
		_, _ = w.Write([]byte(`{"type":"COSTS"}` + "\n"))
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithAPIKey("codex-key"))
	body, err := client.DownloadLog(t.Context(), "org-123", "eclf_123")
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"COSTS"}`, string(body))
}

func TestDownloadLogRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.CopyN(w, zeroReader{}, maxLogFileSize+1)
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithAPIKey("codex-key"))
	_, err := client.DownloadLog(t.Context(), "org-123", "eclf_123")

	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}

func TestListLogsReturnsHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithAPIKey("bad-key"))
	_, err := client.ListLogs(t.Context(), ListLogsParams{PrincipalID: "org-123"})

	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusUnauthorized, httpErr.StatusCode)
	require.Equal(t, "401 Unauthorized", httpErr.Status)
	require.Contains(t, httpErr.Body, "unauthorized")
	require.Contains(t, err.Error(), "unauthorized")
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func testGuardianPolicy(t *testing.T) *guardian.Policy {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	return policy
}
