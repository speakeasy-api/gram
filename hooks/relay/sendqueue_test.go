package relay

import (
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
