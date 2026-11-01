package relay

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"
)

// A token whose token-box record cannot be persisted is not used: without the
// record a later failed identity lookup could fall back to a key. The box
// fails closed, whatever the fail-open posture, until a token persists.
func TestTokenNotUsedWhenTokenBoxCannotBePersisted(t *testing.T) {
	t.Cleanup(func() { tokenPersistFailed.Store(false) })
	fs, _ := modeServer(t)
	cfg, _ := tokenBox(t, fs.URL)
	setSpoolStateHome(t)
	writeOrgSettings(cfg, true)

	// A directory where the record goes makes every write of it fail.
	require.NoError(t, os.MkdirAll(tokenBoxPath(), 0o700))
	requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"),
		"an unpersisted token is not used, even in a fail-open org")
	require.Empty(t, fs.requests, "nothing is sent with an unpersisted token")
	require.Empty(t, listSpoolEntries(spoolDirPath()), "nothing is spooled")
	requireNoKeySent(t, fs)

	require.NoError(t, os.Remove(tokenBoxPath()))
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.NotContains(t, string(res.Stdout), `"permissionDecision":"deny"`, "once the record persists the token is used again")
	require.NotEmpty(t, fs.requests, "the event is sent with the persisted token")
	require.False(t, tokenPersistFailed.Load())
}

// Caching a fresh token never clears a recorded refusal, including when the
// marker file was lost and only the record holds it.
func TestTokenBoxWriteNeverClearsARefusal(t *testing.T) {
	fs, _ := modeServer(t)
	cfg, _ := tokenBox(t, fs.URL)
	argv := []string{"/opt/speakeasy/speakeasyd", "hooks-token"}
	c := creds{ServerURL: cfg.ServerURL, APIKey: "", Project: "hooks-proj", Email: "", Org: cfg.OrgID,
		Source: credToken, Bearer: "tok", ExpiresAt: time.Now().Add(time.Hour)}

	require.NoError(t, writeTokenAuth(c, argv))
	require.NoError(t, markTokenRejected())
	require.NoError(t, os.Remove(tokenRejectedPath()))
	require.NoError(t, writeTokenAuth(c, argv))
	st, ok := readTokenBox()
	require.True(t, ok)
	require.True(t, st.Rejected, "writing a token keeps the refusal")
	require.True(t, tokenRejected())
}

// Concurrent token writes and refusals (a live send racing a spool drain)
// never lose the refusal: the record is updated under a cross-process lock.
func TestConcurrentTokenWritesKeepTheRefusal(t *testing.T) {
	fs, _ := modeServer(t)
	cfg, _ := tokenBox(t, fs.URL)
	argv := []string{"/opt/speakeasy/speakeasyd", "hooks-token"}
	c := creds{ServerURL: cfg.ServerURL, APIKey: "", Project: "hooks-proj", Email: "", Org: cfg.OrgID,
		Source: credToken, Bearer: "tok", ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, writeTokenAuth(c, argv))

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() { defer wg.Done(); _ = writeTokenAuth(c, argv) }()
		go func() { defer wg.Done(); _ = markTokenRejected() }()
	}
	wg.Wait()
	_ = os.Remove(tokenRejectedPath())
	st, ok := readTokenBox()
	require.True(t, ok)
	require.True(t, st.Rejected, "no concurrent token write overwrote a refusal")
}

// New spool entries carry schema v2, which a drain that predates token_project
// skips; this drain still reads v1, but token credentials never replay an
// entry with no project binding.
func TestSpoolSchemaV2AndUnboundV1Entries(t *testing.T) {
	fs, mode := modeServer(t)
	cfg, _ := tokenBox(t, fs.URL)
	stateHome := setSpoolStateHome(t)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	mode.Store(503)
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")
	t.Setenv("XDG_STATE_HOME", stateHome)
	files := listSpoolEntries(spoolDirPath())
	require.Len(t, files, 1)
	path := files[0]
	if !filepath.IsAbs(path) {
		path = filepath.Join(spoolDirPath(), path)
	}
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	require.InDelta(t, 2, raw["v"], 0, "new entries are schema v2")

	// Rewrite it as a pre-binding v1 entry: no token_project.
	raw["v"] = 1
	delete(raw, "token_project")
	b, err = json.Marshal(raw)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o600))

	mode.Store(200)
	sent := len(fs.requests)
	s := Drain(context.Background())
	require.Equal(t, 1, s.Dropped, "an unbound v1 entry is dropped on a token box")
	require.Len(t, fs.requests, sent, "it is never replayed with the token")
	requireNoKeySent(t, fs)
}
