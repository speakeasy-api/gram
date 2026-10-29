package relay

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// modeServer answers every event with the status in mode: 200, 401 or 503.
// It shrinks the SDK's 5xx backoff so a scripted outage costs milliseconds.
func modeServer(t *testing.T) (*fakeServer, *atomic.Int32) {
	t.Helper()
	prev := retryMaxElapsedMS
	retryMaxElapsedMS = 1
	t.Cleanup(func() { retryMaxElapsedMS = prev })
	var mode atomic.Int32
	mode.Store(http.StatusOK)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		switch m := int(mode.Load()); m {
		case http.StatusOK:
			return m, decision{Decision: "allow", Reason: "", Message: ""}
		default:
			return m, decision{Decision: "", Reason: "", Message: "refused"}
		}
	})
	return fs, &mode
}

func requireNoKeySent(t *testing.T, fs *fakeServer) {
	t.Helper()
	for _, h := range fs.headers {
		require.Empty(t, h.Get("Gram-Key"), "a token box never sends a key")
	}
}

// A refused box whose bearer is still cached must not trust it: a later outage
// must not let it fail open or spool, whether or not a fresh token is issued.
func TestRefusedBoxIgnoresCachedTokenDuringAnOutage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fresh error
	}{
		{name: "no fresh token", fresh: errors.New("exit status 1: unreachable")},
		{name: "fresh token but server down", fresh: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, mode := modeServer(t)
			cfg, fc := tokenBox(t, fs.URL)
			stateHome := setSpoolStateHome(t)
			writeOrgSettings(cfg, true)

			invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
			_, cached := readCachedToken(cfg)
			require.True(t, cached, "precondition: a valid bearer is cached")
			require.NoError(t, markTokenRejected())

			mode.Store(http.StatusServiceUnavailable)
			fc.fail = tc.fresh
			requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"),
				"a refused box stays closed during an outage")
			invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")
			t.Setenv("XDG_STATE_HOME", stateHome)
			require.Empty(t, listSpoolEntries(spoolDirPath()), "a refused box keeps nothing for replay")
			requireNoKeySent(t, fs)
		})
	}
}

// A live rejection forgets the rejected bearer, but not the proof that this is
// a token box: with no env command and no device agent to ask, it still never
// uses a key.
func TestLiveRejectionForgetsTheBearerButStaysATokenBox(t *testing.T) {
	fs, mode := modeServer(t)
	cfg, fc := tokenBox(t, fs.URL)
	setSpoolStateHome(t)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	mode.Store(http.StatusUnauthorized)
	requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"), "a rejected token is denied")
	_, cached := readCachedToken(cfg)
	require.False(t, cached, "the rejected bearer is forgotten")

	// Discovery now fails entirely: no env override, no device agent.
	t.Setenv("SPEAKEASY_AI_HOOKS_TOKEN_COMMAND", "")
	resetDeviceIdentityMemo()
	argv, ok := tokenCommandArgv()
	require.True(t, ok, "the token-box record keeps the box on token auth")
	require.Equal(t, []string{"/opt/speakeasy/speakeasyd", "hooks-token"}, argv)

	fc.fail = errors.New("exit status 1")
	requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"), "still closed, still no key")
	requireNoKeySent(t, fs)
}

// A damaged auth cache or token-box record must not reopen key fallback.
func TestDamagedTokenStateFailsClosed(t *testing.T) {
	fs, _ := modeServer(t)
	cfg, fc := tokenBox(t, fs.URL)
	setSpoolStateHome(t)
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.NoError(t, os.WriteFile(authFilePath(), []byte("\x00garbage"), 0o600))
	require.NoError(t, os.WriteFile(tokenBoxPath(), []byte("{not json"), 0o600))
	t.Setenv("SPEAKEASY_AI_HOOKS_TOKEN_COMMAND", "")
	resetDeviceIdentityMemo()

	argv, ok := tokenCommandArgv()
	require.True(t, ok, "an unreadable record still marks a token box")
	require.Empty(t, argv, "with no argv the real command cannot run")
	fc.fail = errors.New("the hooks token command must start with an absolute path")
	require.True(t, tokenRejected(), "an unreadable record counts as refused")
	requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"), "damaged state fails closed")
	requireNoKeySent(t, fs)
}

// The drain applies the live path's recovery: one fresh token and one resend,
// then a recorded refusal, never a key.
func TestDrainRefreshesOnceThenRecordsTheRefusal(t *testing.T) {
	fs, mode := modeServer(t)
	cfg, fc := tokenBox(t, fs.URL)
	stateHome := setSpoolStateHome(t)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	mode.Store(http.StatusServiceUnavailable)
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")
	t.Setenv("XDG_STATE_HOME", stateHome)
	require.Len(t, listSpoolEntries(spoolDirPath()), 1, "precondition: the outage spooled the event")

	mode.Store(http.StatusUnauthorized)
	before := fc.refresh.Load()
	s := Drain(context.Background())
	require.Equal(t, before+1, fc.refresh.Load(), "the drain asks for one fresh token")
	require.Equal(t, 1, s.Skipped, "a rejected entry is kept, not deleted")
	require.True(t, tokenRejected(), "the drain records the refusal like a live send")
	requireNoKeySent(t, fs)
}

// A drain whose token command delivers no configured hooks project replays
// nothing, never through a key, and records the refusal.
func TestDrainWithoutConfiguredProjectReplaysNothing(t *testing.T) {
	fs, mode := modeServer(t)
	cfg, fc := tokenBox(t, fs.URL)
	stateHome := setSpoolStateHome(t)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	mode.Store(http.StatusServiceUnavailable)
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")
	t.Setenv("XDG_STATE_HOME", stateHome)
	require.Len(t, listSpoolEntries(spoolDirPath()), 1)

	forgetAuth()
	fc.project = ""
	mode.Store(http.StatusOK)
	sent := len(fs.requests)
	s := Drain(context.Background())
	require.Equal(t, 1, s.Skipped, "the entry is not replayed")
	require.Len(t, fs.requests, sent, "nothing is sent without a configured hooks project")
	require.True(t, tokenRejected())
	requireNoKeySent(t, fs)
}

// A spooled event bound to one configured hooks project is never replayed
// into another after the box's configured project changes.
func TestDrainDropsEventsBoundToAnotherProject(t *testing.T) {
	fs, mode := modeServer(t)
	cfg, fc := tokenBox(t, fs.URL)
	stateHome := setSpoolStateHome(t)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	mode.Store(http.StatusServiceUnavailable)
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")
	t.Setenv("XDG_STATE_HOME", stateHome)
	require.Len(t, listSpoolEntries(spoolDirPath()), 1)

	forgetAuth()
	fc.project = "other-proj"
	mode.Store(http.StatusOK)
	sent := len(fs.requests)
	s := Drain(context.Background())
	require.Equal(t, 1, s.Dropped, "the entry is dropped")
	require.Len(t, fs.requests, sent, "nothing is replayed into the new project")
}

// A rejected live event is not resent with a fresh token for another project.
func TestLiveRetryNeverCrossesProjects(t *testing.T) {
	fs, mode := modeServer(t)
	cfg, fc := tokenBox(t, fs.URL)
	setSpoolStateHome(t)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	mode.Store(http.StatusUnauthorized)
	fc.project = "other-proj"
	sent := len(fs.requests)
	requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"), "the event stays denied")
	require.Len(t, fs.requests, sent+1, "only the original attempt is sent")
}

func TestTokenCommandEnvWithholdsEveryUnlistedName(t *testing.T) {
	t.Setenv("SPEAKEASY_AI_HOOKS_ORG_KEY", "org-key")
	t.Setenv("SPEAKEASY_AI_LITELLM_INGEST_KEY", "litellm-key")
	t.Setenv("SPEAKEASY_SOMETHING_NEW", "x")
	t.Setenv("SPEAKEASY_MANAGED_CONFIG", "/etc/speakeasy/managed.json")
	t.Setenv("SPEAKEASY_SOCKET", "/run/speakeasy.sock")
	env := strings.Join(tokenCommandEnv(false), "\n")
	require.NotContains(t, env, "ORG_KEY")
	require.NotContains(t, env, "LITELLM")
	require.NotContains(t, env, "SPEAKEASY_SOMETHING_NEW")
	require.Contains(t, env, "SPEAKEASY_MANAGED_CONFIG=/etc/speakeasy/managed.json")
	require.Contains(t, env, "SPEAKEASY_SOCKET=/run/speakeasy.sock")
}

func TestParseTokenOutputRejectsTrailingDelimiters(t *testing.T) {
	_, err := parseTokenOutput([]byte(`{"authorization":"Bearer t","project":"p","expires_at":"2999-01-01T00:00:00Z"}]`))
	require.Error(t, err)
}

func TestSanitizeCommandStderrMasksShortCredentials(t *testing.T) {
	got := sanitizeCommandStderr("refused: bearer abc123 for key=sk_x9")
	require.NotContains(t, got, "abc123")
	require.NotContains(t, got, "sk_x9")
	require.Contains(t, got, "refused:")
}
