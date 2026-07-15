package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// seedSpoolEntry writes a well-formed entry aged `age`, returning its
// idempotency key.
func seedSpoolEntry(t *testing.T, serverURL string, age time.Duration, sessionID string) string {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	key := newIdempotencyToken()
	entry := spoolEntry{
		V:              spoolEntryVersion,
		IdempotencyKey: key,
		ServerURL:      serverURL,
		OrgID:          "",
		ProjectSlug:    "default",
		ConfigPath:     "",
		SpooledAt:      time.Now().Add(-age).UTC(),
		Envelope: components.IngestRequestBody{
			SchemaVersion: schemaVersion,
			Source:        components.HookIngestSource{Adapter: "claude", AdapterVersion: nil, RawEventName: nil, Hostname: nil, UserEmail: nil},
			Session:       &components.HookIngestSession{ID: &sessionID, TurnID: nil, Cwd: nil, Model: nil},
			Event:         components.HookIngestEvent{Type: components.TypeToolRequested, OccurredAt: nil},
			Data:          nil,
			Raw:           nil,
		},
	}
	b, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, spoolFileName(time.Now().Add(-age))), b, 0o600))
	return key
}

func drainEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "drain-key")
}

// TestDrainReplaysOldestFirstWithStoredKeys pins the replay contract: entries
// go out in chronological order, each under its originally minted
// Idempotency-Key, every request carrying the X-Gram-Replayed marker, and
// accepted entries leave the spool.
func TestDrainReplaysOldestFirstWithStoredKeys(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, nil)
	keyOld := seedSpoolEntry(t, fs.URL, 2*time.Hour, "sess-old")
	keyNew := seedSpoolEntry(t, fs.URL, time.Hour, "sess-new")

	s := Drain(t.Context())
	require.Equal(t, DrainSummary{Replayed: 2, Dropped: 0, Expired: 0, Skipped: 0, Remaining: 0, Aborted: false}, s)
	require.Empty(t, spoolFiles(t), "accepted entries must leave the spool")

	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Len(t, fs.requests, 2)
	require.Equal(t, "sess-old", *fs.requests[0].Session.ID, "oldest entry must replay first")
	require.Equal(t, "sess-new", *fs.requests[1].Session.ID)
	require.Equal(t, keyOld, fs.headers[0].Get("Idempotency-Key"), "replay must reuse the original send's key")
	require.Equal(t, keyNew, fs.headers[1].Get("Idempotency-Key"))
	for _, h := range fs.headers {
		require.Equal(t, "1", h.Get("X-Gram-Replayed"), "replayed traffic must be distinguishable from live traffic")
	}
}

// TestDrainAbortsWhenServerStillDown pins backpressure: the first unsent
// exchange stops the run and the backlog survives untouched.
func TestDrainAbortsWhenServerStillDown(t *testing.T) {
	drainEnv(t)
	url := closedPortURL(t)
	seedSpoolEntry(t, url, 2*time.Hour, "sess-1")
	seedSpoolEntry(t, url, time.Hour, "sess-2")

	s := Drain(t.Context())
	require.True(t, s.Aborted)
	require.Zero(t, s.Replayed)
	require.Equal(t, 2, s.Remaining, "an aborted drain must keep the backlog")
	require.Len(t, spoolFiles(t), 2)
}

// TestDrainDropsDefinitiveRejections: a 4xx answer means a replay would fail
// identically — delete rather than wedge the spool.
func TestDrainDropsDefinitiveRejections(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnprocessableEntity, decision{Decision: "", Reason: "", Message: ""}
	})
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-1")

	s := Drain(t.Context())
	require.Equal(t, 1, s.Dropped)
	require.False(t, s.Aborted)
	require.Empty(t, spoolFiles(t))
}

// TestDrainExpiresStaleEntriesWithoutSending: entries past spoolMaxAge are
// deleted unsent — replaying two-week-old telemetry would only distort
// session analytics.
func TestDrainExpiresStaleEntriesWithoutSending(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, spoolMaxAge+time.Hour, "sess-stale")

	s := Drain(t.Context())
	require.Equal(t, 1, s.Expired)
	require.Zero(t, fs.count(), "expired entries must never reach the server")
	require.Empty(t, spoolFiles(t))
}

// TestDrainSkipsNewerSchemaEntries: an entry written by a newer binary is
// left for a newer binary — never interpreted, never deleted.
func TestDrainSkipsNewerSchemaEntries(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, nil)
	key := seedSpoolEntry(t, fs.URL, time.Hour, "sess-1")
	_ = key
	names := spoolFiles(t)
	require.Len(t, names, 1)
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	path := filepath.Join(dir, names[0])
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	raw["v"] = spoolEntryVersion + 1
	b, err = json.Marshal(raw)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o600))

	s := Drain(t.Context())
	require.Equal(t, 1, s.Skipped)
	require.Equal(t, 1, s.Remaining)
	require.Zero(t, fs.count())
}

// TestDrainDropsCorruptEntries: an unparseable file can never replay;
// keeping it would wedge the head of the spool forever.
func TestDrainDropsCorruptEntries(t *testing.T) {
	drainEnv(t)
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, spoolFileName(time.Now())), []byte("{corrupt"), 0o600))

	s := Drain(t.Context())
	require.Equal(t, 1, s.Dropped)
	require.Empty(t, spoolFiles(t))
}

// TestDrainSkipsWithoutCredentials: no env key, no cache, no config org key
// → the entry stays for a run that can authenticate, and the exit code says
// the run was incomplete.
func TestDrainSkipsWithoutCredentials(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-1")

	var out bytes.Buffer
	code := RunDrain(t.Context(), &out)
	require.Equal(t, 1, code, "an incomplete drain must exit non-zero for the supervising caller's log")
	require.Contains(t, out.String(), "skipped=1")
	require.Len(t, spoolFiles(t), 1)
	require.Zero(t, fs.count())
}

// TestDrainUsesConfigOrgKeyFallback: with no env key and no cache, the
// entry's recorded config path supplies the shared org key — the same
// fallback a live send has.
func TestDrainUsesConfigOrgKeyFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	fs := newFakeServer(t, nil)

	cfgPath := filepath.Join(t.TempDir(), "speakeasy.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"server_url":"`+fs.URL+`","project":"default","hooks_api_key":"org-key-1"}`), 0o600))

	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	sessionID := "sess-1"
	entry := spoolEntry{
		V:              spoolEntryVersion,
		IdempotencyKey: newIdempotencyToken(),
		ServerURL:      fs.URL,
		OrgID:          "",
		ProjectSlug:    "default",
		ConfigPath:     cfgPath,
		SpooledAt:      time.Now().UTC(),
		Envelope: components.IngestRequestBody{
			SchemaVersion: schemaVersion,
			Source:        components.HookIngestSource{Adapter: "claude", AdapterVersion: nil, RawEventName: nil, Hostname: nil, UserEmail: nil},
			Session:       &components.HookIngestSession{ID: &sessionID, TurnID: nil, Cwd: nil, Model: nil},
			Event:         components.HookIngestEvent{Type: components.TypeToolRequested, OccurredAt: nil},
			Data:          nil,
			Raw:           nil,
		},
	}
	b, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, spoolFileName(time.Now())), b, 0o600))

	s := Drain(t.Context())
	require.Equal(t, 1, s.Replayed)
	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Equal(t, "org-key-1", fs.headers[0].Get("Gram-Key"))
}

// TestMaybeSpawnDrainAfterRecovery drives the real provider path: sends fail
// against a dead server (payloads spool), the server comes back, and the
// next successful send spawns exactly one detached drain — debounced across
// a burst.
func TestMaybeSpawnDrainAfterRecovery(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	spawns := 0
	orig := startDrainProcess
	startDrainProcess = func() error { spawns++; return nil }
	t.Cleanup(func() { startDrainProcess = orig })

	// Outage: the payload spools, and no drain spawns off a failed send.
	cfg := authedConfig(t, closedPortURL(t))
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Len(t, spoolFiles(t), 1)
	require.Zero(t, spawns, "a failed send must not spawn a drain")

	// Recovery: a successful send with a non-empty spool spawns the drain.
	fs := newFakeServer(t, nil)
	cfg.ServerURL = fs.URL
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, 1, spawns, "the first healthy send after an outage must flush the backlog")

	// Burst: the debounce marker suppresses a second spawn.
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, 1, spawns, "a burst of healthy sends must not stack drains")
}

// TestMaybeSpawnDrainSkipsEmptySpool: healthy sends on a machine with no
// backlog never pay the spawn cost.
func TestMaybeSpawnDrainSkipsEmptySpool(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	spawns := 0
	orig := startDrainProcess
	startDrainProcess = func() error { spawns++; return nil }
	t.Cleanup(func() { startDrainProcess = orig })

	fs := newFakeServer(t, nil)
	invoke(t, authedConfig(t, fs.URL), agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Zero(t, spawns)
}

// TestRunDrainExitCodes: 0 when the spool ends empty, 1 when work remains.
func TestRunDrainExitCodes(t *testing.T) {
	drainEnv(t)
	var out bytes.Buffer
	require.Equal(t, 0, RunDrain(context.Background(), &out), "an empty spool is a clean run")

	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-1")
	out.Reset()
	require.Equal(t, 0, RunDrain(context.Background(), &out))
	require.Contains(t, out.String(), "replayed=1")
}
