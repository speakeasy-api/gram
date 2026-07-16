package relay

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// These tests pin the ordered send-queue behavior (DNO-536): while the spool
// holds outage backlog, observe-only events queue behind it instead of
// overtaking it, and gating events flush the backlog (bounded) before their
// live verdict send — so replayed messages land in conversation order, not
// drain order.

func stubDrainSpawn(t *testing.T) *int {
	t.Helper()
	spawns := 0
	orig := startDrainProcess
	startDrainProcess = func() error { spawns++; return nil }
	t.Cleanup(func() { startDrainProcess = orig })
	return &spawns
}

// TestObserveEventQueuesBehindBacklog: an observe-only event arriving while
// the spool is non-empty must not send live (it would persist ahead of older
// messages); it appends to the spool and kicks the drain.
func TestObserveEventQueuesBehindBacklog(t *testing.T) {
	drainEnv(t)
	spawns := stubDrainSpawn(t)
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-backlog")
	cfg := authedConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Zero(t, fs.count(), "a queued observe event must not reach the server live")
	require.Len(t, spoolFiles(t), 2, "the event must append behind the existing backlog")
	require.Equal(t, 1, *spawns, "queueing must kick the drain so the queue empties while the server is up")
}

// TestObserveEventSendsLiveWhenSpoolEmpty: the queue path only engages while
// backlog exists — the everyday case stays a direct live send with nothing
// written to disk.
func TestObserveEventSendsLiveWhenSpoolEmpty(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count())
	require.Empty(t, spoolFiles(t))
}

// TestObserveEventWithBacklogAndNoCredentialsDoesNotQueue: an
// unauthenticated install never leaks events — not to the network, and not
// into another deployment's backlog either.
func TestObserveEventWithBacklogAndNoCredentialsDoesNotQueue(t *testing.T) {
	setSpoolStateHome(t)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-backlog")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Zero(t, fs.count())
	require.Len(t, spoolFiles(t), 1, "no-credential machines must not append to the queue")
}

// TestGatingEventDrainsBacklogBeforeVerdict pins the DNO-536 repro: the
// first live event after an outage must not persist ahead of the spooled
// messages that happened before it. The gating event flushes the backlog
// synchronously, then sends live — the server sees oldest, newer, live, in
// that order, with only the replays carrying the marker.
func TestGatingEventDrainsBacklogBeforeVerdict(t *testing.T) {
	drainEnv(t)
	stubDrainSpawn(t)
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, 2*time.Hour, "sess-old")
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-new")
	cfg := authedConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Empty(t, spoolFiles(t), "the backlog must be flushed before the verdict send")

	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Len(t, fs.requests, 3, "two replays then the live gating event")
	require.Equal(t, "sess-old", *fs.requests[0].Session.ID)
	require.Equal(t, "sess-new", *fs.requests[1].Session.ID)
	for i := range 2 {
		marker, err := strconv.ParseBool(fs.headers[i].Get("X-Gram-Replayed"))
		require.NoError(t, err)
		require.True(t, marker, "backlog replays must carry the replay marker")
	}
	require.Equal(t, components.TypeToolRequested, fs.requests[2].Event.Type)
	require.Empty(t, fs.headers[2].Get("X-Gram-Replayed"), "the live event is not a replay")
}

// TestGatingEventProceedsWhenDrainLockHeld: a drain already in flight must
// not stall the verdict — the gating event skips the flush and sends live.
func TestGatingEventProceedsWhenDrainLockHeld(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the drain lock is a no-op on Windows")
	}
	drainEnv(t)
	stubDrainSpawn(t)
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-backlog")
	cfg := authedConfig(t, fs.URL)

	lockPath := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool", "drain.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.NoError(t, lockFile(f))
	defer unlockFile(f)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count(), "the live verdict send must proceed without the flush")
	require.Equal(t, components.TypeToolRequested, fs.last().Event.Type)
	require.Len(t, spoolFiles(t), 1, "the held lock leaves the backlog to the drain that owns it")
}

// TestGatingEventBudgetExpiryStillSendsLive: a backlog too slow for the
// budget must not block the user — the flush aborts at the budget and the
// live verdict send proceeds; the unsent entry stays for a later drain.
func TestGatingEventBudgetExpiryStillSendsLive(t *testing.T) {
	drainEnv(t)
	stubDrainSpawn(t)
	origBudget := gatingDrainBudget
	gatingDrainBudget = 50 * time.Millisecond
	t.Cleanup(func() { gatingDrainBudget = origBudget })

	fs := newFakeServer(t, func(p components.IngestRequestBody) (int, decision) {
		if p.Session != nil && p.Session.ID != nil && *p.Session.ID == "sess-slow" {
			time.Sleep(300 * time.Millisecond) // outlives the budget; the replay send is canceled
		}
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-slow")
	cfg := authedConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Contains(t, string(res.Stdout), "{}", "the verdict must resolve despite the slow backlog")
	require.Len(t, spoolFiles(t), 1, "the undelivered entry must survive for a later drain")

	fs.mu.Lock()
	defer fs.mu.Unlock()
	var liveSeen bool
	for _, p := range fs.requests {
		if p.Session == nil || p.Session.ID == nil || *p.Session.ID != "sess-slow" {
			liveSeen = true
		}
	}
	require.True(t, liveSeen, "the live gating event must still reach the server for its verdict")
}

// seedPoisonSpoolEntry writes an entry with a newer schema version — one this
// binary can never deliver, only skip.
func seedPoisonSpoolEntry(t *testing.T, serverURL string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	entry := map[string]any{
		"v":               spoolEntryVersion + 1,
		"idempotency_key": newIdempotencyToken(),
		"server_url":      serverURL,
	}
	b, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, spoolFileName(time.Now().Add(-time.Hour))), b, 0o600))
}

// TestObserveEventIgnoresUndrainableOnlyBacklog: a poison entry (newer schema
// version) is skipped forever and lingers until the 14-day expiry. Once a
// drain run has proved it undrainable, it must stop counting as backlog —
// otherwise every observe event queues behind it for two weeks.
func TestObserveEventIgnoresUndrainableOnlyBacklog(t *testing.T) {
	drainEnv(t)
	stubDrainSpawn(t)
	fs := newFakeServer(t, nil)
	seedPoisonSpoolEntry(t, fs.URL)
	cfg := authedConfig(t, fs.URL)

	// A drain run walks the spool, skips the poison entry, and records it as
	// undrainable for this binary.
	s := Drain(t.Context())
	require.Equal(t, 1, s.Skipped)
	require.Equal(t, 1, s.Remaining)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count(), "an undrainable-only spool must not reroute observe events through the queue")
	require.Len(t, spoolFiles(t), 1, "the poison entry stays for a newer binary or the age cap")
}

// TestQueueFullFallsBackToLiveSend: when queueing would evict older backlog
// at the caps, the observe event sends live instead — dropping the oldest
// conversation rows to admit a newer event would invert the ordering goal,
// and dropping the event loses data the reachable server could store.
func TestQueueFullFallsBackToLiveSend(t *testing.T) {
	drainEnv(t)
	stubDrainSpawn(t)
	origCap := spoolEntryCap
	spoolEntryCap = 1
	t.Cleanup(func() { spoolEntryCap = origCap })

	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-oldest")
	before := spoolFiles(t)
	cfg := authedConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count(), "the event must send live when the queue is at capacity")
	require.Equal(t, before, spoolFiles(t), "the oldest backlog entry must never be evicted to admit a newer event")
}

// TestQueueSpawnDebouncedAcrossBurst: a burst of queued observe events must
// not stack detached drain processes — the last-spawn marker debounces to
// one useful run.
func TestQueueSpawnDebouncedAcrossBurst(t *testing.T) {
	drainEnv(t)
	spawns := stubDrainSpawn(t)
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-backlog")
	cfg := authedConfig(t, fs.URL)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")

	require.Equal(t, 1, *spawns, "a burst of queued events must spawn one drain, not one per event")
	require.Len(t, spoolFiles(t), 3, "both burst events still queue behind the backlog")
}

// TestDrainPicksUpEntriesAppendedMidPass: drainUntilDry re-lists after a
// productive pass, so an event queued while the drain was already running —
// a hook racing the flush — is delivered by the same run instead of
// stranding until the next trigger.
func TestDrainPicksUpEntriesAppendedMidPass(t *testing.T) {
	drainEnv(t)
	appended := false
	var fs *fakeServer
	fs = newFakeServer(t, func(p components.IngestRequestBody) (int, decision) {
		if !appended && p.Session != nil && p.Session.ID != nil && *p.Session.ID == "sess-a" {
			appended = true
			seedSpoolEntry(t, fs.URL, 0, "sess-b")
		}
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-a")

	s := Drain(t.Context())

	require.Equal(t, 2, s.Replayed, "the mid-pass append must drain in the same run")
	require.Zero(t, s.Remaining)
	require.False(t, s.Aborted)
	require.Empty(t, spoolFiles(t))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Len(t, fs.requests, 2)
	require.Equal(t, "sess-a", *fs.requests[0].Session.ID)
	require.Equal(t, "sess-b", *fs.requests[1].Session.ID, "the appended entry must replay after the backlog")
}
