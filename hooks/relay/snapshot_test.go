package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

func setSnapshotNow(t *testing.T, now time.Time) {
	t.Helper()
	prev := snapshotNow
	snapshotNow = func() time.Time { return now }
	t.Cleanup(func() { snapshotNow = prev })
}

func TestDailySnapshotRidesFirstEventOfEachDay(t *testing.T) {
	setSpoolStateHome(t)
	day := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	setSnapshotNow(t, day)

	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	cfg.DebugLog = filepath.Join(t.TempDir(), "debug.log")

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	first := fs.headers[0]
	require.Equal(t, "true", first.Get("X-Gram-Device-Debug-Log"))
	require.Equal(t, "0", first.Get("X-Gram-Device-Spool-Depth"))
	require.Contains(t, []string{"true", "false"}, first.Get("X-Gram-Device-Fail-Open"))

	sent := fs.count()
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Greater(t, fs.count(), sent)
	require.Empty(t, fs.headers[sent].Get("X-Gram-Device-Fail-Open"), "one snapshot per day")

	setSnapshotNow(t, day.Add(24*time.Hour))
	sent = fs.count()
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.NotEmpty(t, fs.headers[sent].Get("X-Gram-Device-Fail-Open"), "a new day sends a new snapshot")
}

func TestDailySnapshotRetriesAfterFailedSend(t *testing.T) {
	setSpoolStateHome(t)
	setSnapshotNow(t, time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC))

	status := http.StatusInternalServerError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	send := func() {
		ctx := withDailySnapshot(context.Background(), Config{})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, nil)
		require.NoError(t, err)
		res, err := (&deviceTransport{base: http.DefaultTransport}).RoundTrip(req)
		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
	}

	send()
	_, err := os.Stat(snapshotMarkerPath())
	require.ErrorIs(t, err, os.ErrNotExist, "a failed send must not use up the day's snapshot")

	status = http.StatusOK
	send()
	b, err := os.ReadFile(snapshotMarkerPath())
	require.NoError(t, err)
	require.Equal(t, "2026-11-09", string(b))
	_, ok := withDailySnapshot(context.Background(), Config{}).Value(clientSnapshotKey{}).(clientSnapshot)
	require.False(t, ok)
}

func TestSpoolDepthCountsOnlyEntries(t *testing.T) {
	setSpoolStateHome(t)
	require.Zero(t, spoolDepth())

	dir := spoolDir()
	require.NotEmpty(t, dir)
	now := time.Now()
	for i := range 2 {
		require.NoError(t, os.WriteFile(filepath.Join(dir, spoolFileName(now.Add(time.Duration(i)))), []byte("{}"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, spoolFileName(now)+".tmp"), []byte("{}"), 0o600))

	require.Equal(t, 2, spoolDepth())
}
