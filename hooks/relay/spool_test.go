package relay

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// spoolFiles returns the conforming entry filenames under the test's spool
// dir, sorted — the same filter the drain applies, so lock/marker siblings
// don't count as entries.
func spoolFiles(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	return listSpoolEntries(dir)
}

func readSpoolEntry(t *testing.T, name string) spoolEntry {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	b, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	var entry spoolEntry
	require.NoError(t, json.Unmarshal(b, &entry))
	return entry
}

// closedPortURL returns an http URL on which nothing is listening, so every
// send fails at the transport (statusCode 0) — the common downtime signature.
// Loopback http is permitted by insecureServerURL.
func closedPortURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	url := "http://" + ln.Addr().String()
	require.NoError(t, ln.Close())
	return url
}

// TestSpoolCapturesUnreachableSend drives the real provider path against a
// dead control plane and pins the DNO-498 capture contract: exactly one
// entry lands in the spool, carrying the deployment identity, a non-empty
// idempotency key, and an envelope that round-trips with the event intact.
func TestSpoolCapturesUnreachableSend(t *testing.T) {
	setSpoolStateHome(t)
	cfg := authedConfig(t, closedPortURL(t))
	cfg.OrgID = "org-1"
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	names := spoolFiles(t)
	require.Len(t, names, 1, "an unreachable send must spool exactly one entry")

	entry := readSpoolEntry(t, names[0])
	require.Equal(t, spoolEntryVersion, entry.V)
	require.NotEmpty(t, entry.IdempotencyKey, "replay needs the original send's idempotency key")
	require.Equal(t, cfg.ServerURL, entry.ServerURL)
	require.Equal(t, "org-1", entry.OrgID)
	require.Equal(t, "default", entry.ProjectSlug)
	require.WithinDuration(t, time.Now(), entry.SpooledAt, time.Minute)
	require.Equal(t, components.TypeToolRequested, entry.Envelope.Event.Type)
	require.NotNil(t, entry.Envelope.Session)
	require.Equal(t, "sess-claude-1", *entry.Envelope.Session.ID)
}

// assertNoSpoolForStatus pins the other half of the predicate: a server that
// answered — success or a definitive 4xx — must not spool, since the event
// was either stored or would be rejected identically on replay.
func assertNoSpoolForStatus(t *testing.T, status int) {
	t.Helper()
	setSpoolStateHome(t)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return status, decision{Decision: "allow", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.NotZero(t, fs.count(), "precondition: the server must have been reached")
	require.Empty(t, spoolFiles(t))
}

func TestSpoolSkipsWhenServerAllows(t *testing.T) {
	assertNoSpoolForStatus(t, http.StatusOK)
}

func TestSpoolSkipsWhenServerRejects4xx(t *testing.T) {
	assertNoSpoolForStatus(t, http.StatusBadRequest)
}

// TestSpoolSkipsWithoutCredentials pins that the ratchet still runs first: a
// never-authenticated machine sends nothing, so it must spool nothing — the
// spool is a delivery buffer, not a credential bypass.
func TestSpoolSkipsWithoutCredentials(t *testing.T) {
	setSpoolStateHome(t)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	cfg := Config{ServerURL: closedPortURL(t), ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Empty(t, spoolFiles(t))
}

// TestSendUsesCallerIdempotencyKey pins the plumbing the spool relies on:
// the key deliver mints is the one that reaches the wire, so the key stored
// in a spool entry dedupes against any partially delivered original.
func TestSendUsesCallerIdempotencyKey(t *testing.T) {
	fs := newFakeServer(t, nil)
	cl := newClient(fs.URL)
	key := newIdempotencyToken()
	cl.send(t.Context(), creds{ServerURL: "", APIKey: "k", Project: "p", Email: "", Org: "", Source: credEnv}, components.IngestRequestBody{
		SchemaVersion: schemaVersion,
		Source:        components.HookIngestSource{Adapter: "claude", AdapterVersion: nil, RawEventName: nil, Hostname: nil, UserEmail: nil},
		Session:       nil,
		Event:         components.HookIngestEvent{Type: components.TypeSessionUpdated, OccurredAt: nil},
		Data:          nil,
		Raw:           nil,
	}, key)

	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Len(t, fs.headers, 1)
	require.Equal(t, key, fs.headers[0].Get("Idempotency-Key"))
}

func TestUnsentPredicate(t *testing.T) {
	for status, want := range map[int]bool{
		0: true, 500: true, 502: true, 503: true,
		http.StatusTooManyRequests: true, http.StatusRequestTimeout: true,
		200: false, 400: false, 401: false, 403: false, 404: false,
	} {
		got := ingestResult{statusCode: status, decision: decision{Decision: "", Reason: "", Message: ""}, authRejected: false}.unsent()
		require.Equal(t, want, got, "unsent(status=%d)", status)
	}
}

func TestSpoolFileNamesSortChronologically(t *testing.T) {
	base := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	a := spoolFileName(base)
	b := spoolFileName(base.Add(time.Nanosecond))
	c := spoolFileName(base.Add(time.Hour))
	require.Less(t, a, b)
	require.Less(t, b, c)

	nanos, ok := spoolNanos(a)
	require.True(t, ok)
	require.Equal(t, base.UnixNano(), nanos)
}

// writeSpoolFixture drops a raw entry file with a name encoding age, for trim
// tests.
func writeSpoolFixture(t *testing.T, dir string, age time.Duration, size int) string {
	t.Helper()
	name := spoolFileName(time.Now().Add(-age))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600))
	return name
}

func TestTrimSpoolCountCap(t *testing.T) {
	dir := t.TempDir()
	var names []string
	for i := range 5 {
		names = append(names, writeSpoolFixture(t, dir, time.Duration(5-i)*time.Minute, 10))
	}

	dropped := trimSpool(dir, 10, time.Now(), 3, 1<<20)
	require.Equal(t, 3, dropped, "5 present + 1 incoming under cap 3 → drop 3 oldest")

	des, err := os.ReadDir(dir)
	require.NoError(t, err)
	var remaining []string
	for _, de := range des {
		remaining = append(remaining, de.Name())
	}
	require.ElementsMatch(t, names[3:], remaining, "the newest entries must survive")
}

func TestTrimSpoolBytesCap(t *testing.T) {
	dir := t.TempDir()
	writeSpoolFixture(t, dir, 3*time.Minute, 400)
	keep := writeSpoolFixture(t, dir, time.Minute, 400)

	// 800 on disk + 400 incoming over a 1000-byte cap → drop the oldest.
	dropped := trimSpool(dir, 400, time.Now(), 100, 1000)
	require.Equal(t, 1, dropped)

	des, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, des, 1)
	require.Equal(t, keep, des[0].Name())
}

func TestTrimSpoolExpiresOldEntries(t *testing.T) {
	dir := t.TempDir()
	writeSpoolFixture(t, dir, spoolMaxAge+time.Hour, 10)
	keep := writeSpoolFixture(t, dir, time.Hour, 10)

	dropped := trimSpool(dir, 10, time.Now(), 100, 1<<20)
	require.Equal(t, 1, dropped, "entries older than spoolMaxAge must expire even under the caps")

	des, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, des, 1)
	require.Equal(t, keep, des[0].Name())
}

func TestTrimSpoolLeavesForeignFilesAlone(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.txt"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "not-nanos.json"), []byte("x"), 0o600))
	// A digits-prefixed foreign file must not parse as an entry either —
	// only the exact writer-produced shape counts.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "01234567890123456789-report.json"), []byte("x"), 0o600))
	for i := range 3 {
		writeSpoolFixture(t, dir, time.Duration(i)*time.Minute, 10)
	}

	dropped := trimSpool(dir, 10, time.Now(), 1, 1<<20)
	require.Equal(t, 3, dropped, "all conforming entries must drop to fit cap 1")

	des, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, des, 3, "files that don't follow the naming scheme are not the trimmer's to delete")
}

// TestSpoolEntriesAccumulateAcrossEvents pins ordering: successive failed
// sends append distinct files whose sorted order is delivery order, the
// invariant the drain's oldest-first replay depends on.
func TestSpoolEntriesAccumulateAcrossEvents(t *testing.T) {
	setSpoolStateHome(t)
	cfg := authedConfig(t, closedPortURL(t))
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	names := spoolFiles(t)
	require.Len(t, names, 2)
	first, second := readSpoolEntry(t, names[0]), readSpoolEntry(t, names[1])
	require.NotEqual(t, first.IdempotencyKey, second.IdempotencyKey, "each event replays under its own key")
	require.False(t, second.SpooledAt.Before(first.SpooledAt), fmt.Sprintf("sorted filenames must be chronological: %v then %v", first.SpooledAt, second.SpooledAt))
}

// TestSpoolOversizeEntryShedsRawNotBacklog pins the per-entry cap: a giant
// event first drops its Raw debug echo, and must never evict the existing
// backlog to make room arithmetic can't make.
func TestSpoolOversizeEntryShedsRawNotBacklog(t *testing.T) {
	dir := setSpoolStateHome(t)
	spoolDir := filepath.Join(dir, "gram", "hooks", "spool")
	require.NoError(t, os.MkdirAll(spoolDir, 0o700))
	existing := writeSpoolFixture(t, spoolDir, time.Hour, 10)

	big := make([]byte, maxSpoolEntryBytes+1024)
	for i := range big {
		big[i] = 'a'
	}
	payload := components.IngestRequestBody{
		SchemaVersion: schemaVersion,
		Source:        components.HookIngestSource{Adapter: "claude", AdapterVersion: nil, RawEventName: nil, Hostname: nil, UserEmail: nil},
		Session:       nil,
		Event:         components.HookIngestEvent{Type: components.TypeToolCompleted, OccurredAt: nil},
		Data:          nil,
		Raw:           json.RawMessage(`{"blob":"` + string(big) + `"}`),
	}
	NewRelay(Config{ServerURL: "https://gram.test", ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}).
		spoolUnsent(newIdempotencyToken(), payload, nil)

	names := spoolFiles(t)
	require.Len(t, names, 2, "the oversize entry must be stored (raw stripped) and the backlog preserved")
	require.Contains(t, names, existing, "pre-existing backlog must survive an oversize write")
	for _, name := range names {
		if name == existing {
			continue
		}
		entry := readSpoolEntry(t, name)
		require.Nil(t, entry.Envelope.Raw, "the raw echo is shed to fit the per-entry cap")
		require.Equal(t, components.TypeToolCompleted, entry.Envelope.Event.Type, "structured fields survive the shed")
	}
}

// TestTrimSpoolSweepsStaleTmpOrphans: a writer killed between write and
// rename leaves a .tmp invisible to the caps; old orphans are swept, fresh
// ones (a live writer may own them) are left alone.
func TestTrimSpoolSweepsStaleTmpOrphans(t *testing.T) {
	dir := t.TempDir()
	stale := spoolFileName(time.Now().Add(-2*time.Hour)) + ".tmp"
	fresh := spoolFileName(time.Now()) + ".tmp"
	require.NoError(t, os.WriteFile(filepath.Join(dir, stale), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, fresh), []byte("x"), 0o600))

	trimSpool(dir, 10, time.Now(), 100, 1<<20)

	_, staleErr := os.Stat(filepath.Join(dir, stale))
	require.True(t, os.IsNotExist(staleErr), "stale .tmp orphans must be swept")
	_, freshErr := os.Stat(filepath.Join(dir, fresh))
	require.NoError(t, freshErr, "fresh .tmp files may still belong to a live writer")
}

// TestLiveSendOmitsReplayMarker pins the other half of the replay contract:
// the marker rides client state now (not a drain-only transport), so live
// traffic must provably not carry it — a flipped default would grant every
// live event the long idempotency window and tag it as downtime backlog,
// and no other test inspects the header on the live path.
func TestLiveSendOmitsReplayMarker(t *testing.T) {
	fs := newFakeServer(t, nil)
	cl := newClient(fs.URL)
	cl.send(t.Context(), creds{ServerURL: "", APIKey: "k", Project: "p", Email: "", Org: "", Source: credEnv}, components.IngestRequestBody{
		SchemaVersion: schemaVersion,
		Source:        components.HookIngestSource{Adapter: "claude", AdapterVersion: nil, RawEventName: nil, Hostname: nil, UserEmail: nil},
		Session:       nil,
		Event:         components.HookIngestEvent{Type: components.TypeSessionUpdated, OccurredAt: nil},
		Data:          nil,
		Raw:           nil,
	}, newIdempotencyToken())

	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Len(t, fs.headers, 1)
	require.Empty(t, fs.headers[0].Values("X-Gram-Replayed"), "live sends must not carry the replay marker")
}
