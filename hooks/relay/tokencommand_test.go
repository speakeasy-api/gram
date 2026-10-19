package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// fakeTokenCommand scripts the token command and the relay clock for a test.
type fakeTokenCommand struct {
	calls    atomic.Int32
	refresh  atomic.Int32
	now      time.Time
	life     time.Duration
	project  string
	fail     error
	raw      []byte
	lastSeen string
}

// tokenBox configures a workload box: a token command, plus an env key, a
// cached personal key and a baked org key that must all be ignored.
func tokenBox(t *testing.T, serverURL string) (Config, *fakeTokenCommand) {
	t.Helper()
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	t.Setenv("SPEAKEASY_AI_HOOKS_AUTH_FILE", authFile)
	t.Setenv("SPEAKEASY_AI_HOOKS_TOKEN_COMMAND", "/opt/speakeasy/speakeasyd hooks-token")
	t.Setenv("SPEAKEASY_AI_HOOKS_API_KEY", "env-key-must-not-be-used")
	t.Setenv("SPEAKEASY_AI_HOOKS_DISABLE_LOCAL_AUTH", "")
	// No real device agent may answer identity: the env command is the source.
	t.Setenv("SPEAKEASY_AI_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")
	resetDeviceIdentityMemo()
	t.Cleanup(resetDeviceIdentityMemo)

	fc := &fakeTokenCommand{now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), life: 15 * time.Minute, project: "hooks-proj"}
	prevRun, prevNow := runTokenCommand, nowFunc
	runTokenCommand = func(_ context.Context, argv []string, refresh bool) ([]byte, error) {
		n := fc.calls.Add(1)
		if refresh {
			fc.refresh.Add(1)
		}
		fc.lastSeen = strings.Join(argv, "\x00")
		if fc.fail != nil {
			return nil, fc.fail
		}
		if fc.raw != nil {
			return fc.raw, nil
		}
		return fmt.Appendf(nil, `{"authorization":"Bearer tok-%d","project":%q,"expires_at":%q}`,
			n, fc.project, fc.now.Add(fc.life).Format(time.RFC3339)), nil
	}
	nowFunc = func() time.Time { return fc.now }
	t.Cleanup(func() { runTokenCommand, nowFunc = prevRun, prevNow })

	cfg := Config{ServerURL: serverURL, ProjectSlug: "default", OrgID: "org-1", HooksAPIKey: "baked-org-key", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	return cfg, fc
}

func TestTokenCommandIsExclusive(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc := tokenBox(t, fs.URL)
	// A personal key cached for this deployment must not be used either.
	require.NoError(t, os.WriteFile(authFilePath(), []byte("server_url="+fs.URL+"\napi_key=cached-key\nproject=default\norg=org-1\n"), 0o600))

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", strings.TrimSpace(string(res.Stdout)))
	require.Equal(t, 1, fs.count())
	h := fs.headers[0]
	require.Equal(t, "Bearer tok-1", h.Get("Authorization"))
	require.Empty(t, h.Get("Gram-Key"), "no key may ride alongside the device token")
	require.Equal(t, "hooks-proj", h.Get("Gram-Project"), "the token's project overrides the plugin's")
	require.Equal(t, int32(1), fc.calls.Load())
	require.Equal(t, "/opt/speakeasy/speakeasyd\x00hooks-token", fc.lastSeen, "the env command line is split on whitespace")
}

func TestTokenCommandCacheHitSkipsCommand(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc := tokenBox(t, fs.URL)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")

	require.Equal(t, int32(1), fc.calls.Load(), "a cached token must not re-run the command")
	require.Equal(t, 2, fs.count())
	require.Equal(t, "Bearer tok-1", fs.headers[1].Get("Authorization"))
	values, err := readAuthFile(authFilePath())
	require.NoError(t, err)
	require.Equal(t, "token", values["kind"])
	require.Empty(t, values["api_key"])
	info, err := os.Stat(authFilePath())
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	require.True(t, authEstablished(), "a cached token establishes the machine")
}

func TestTokenCommandRefreshesNearExpiry(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc := tokenBox(t, fs.URL)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	fc.now = fc.now.Add(14*time.Minute + 30*time.Second) // inside the 60s margin
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, int32(2), fc.calls.Load())
	require.Equal(t, int32(0), fc.refresh.Load(), "expiry is a normal fetch, not a forced refresh")
	require.Equal(t, "Bearer tok-2", fs.headers[1].Get("Authorization"))
}

func TestTokenRejectedRefreshesAndRetriesOnce(t *testing.T) {
	var requests atomic.Int32
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		if requests.Add(1) == 1 {
			return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: "revoked token"}
		}
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	cfg, fc := tokenBox(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, "{}", strings.TrimSpace(string(res.Stdout)))
	require.Equal(t, 2, fs.count())
	require.Equal(t, "Bearer tok-1", fs.headers[0].Get("Authorization"))
	require.Equal(t, "Bearer tok-2", fs.headers[1].Get("Authorization"))
	require.Equal(t, int32(1), fc.refresh.Load(), "the retry must ask for a fresh token")
	require.NoFileExists(t, authFilePath()+".reauth-needed", "a token box never asks a person to sign in")
}

func TestTokenRejectedTwiceFailsClosedWithoutKeyFallback(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: "device revoked"}
	})
	cfg, _ := tokenBox(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Equal(t, 2, fs.count(), "one retry, then stop")
	for _, h := range fs.headers {
		require.Empty(t, h.Get("Gram-Key"), "a rejected token must never fall back to a key")
	}
}

func TestTokenUnavailableSpoolsObserveOnlyEvent(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc := tokenBox(t, fs.URL)
	fc.fail = errors.New("daemon unreachable")
	stateHome := setSpoolStateHome(t)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 0, fs.count(), "nothing is sent without a token")
	t.Setenv("XDG_STATE_HOME", stateHome)
	require.Len(t, listSpoolEntries(spoolDirPath()), 1, "the event record is kept for replay")
}

func TestTokenUnavailableGatingFollowsFailOpenPosture(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failOpen bool
		denied   bool
	}{
		{name: "org fails open", failOpen: true, denied: false},
		{name: "org fails closed", failOpen: false, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeServer(t, nil)
			cfg, fc := tokenBox(t, fs.URL)
			fc.fail = errors.New("daemon unreachable")
			setSpoolStateHome(t)
			writeOrgSettings(cfg, tc.failOpen)

			res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

			if tc.denied {
				require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
				require.Contains(t, string(res.Stdout), "credential-unavailable")
			} else {
				require.Equal(t, "{}", strings.TrimSpace(string(res.Stdout)))
			}
			require.Equal(t, 0, fs.count())
		})
	}
}

func TestParseTokenOutputRejectsMalformedOutput(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	prev := nowFunc
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = prev })
	future := now.Add(time.Hour).Format(time.RFC3339)

	for _, tc := range []struct{ name, raw string }{
		{name: "not json", raw: "Bearer abc"},
		{name: "not a bearer", raw: `{"authorization":"Basic abc","project":"p","expires_at":"` + future + `"}`},
		{name: "empty token", raw: `{"authorization":"Bearer ","project":"p","expires_at":"` + future + `"}`},
		{name: "token with a newline", raw: `{"authorization":"Bearer a\nb","project":"p","expires_at":"` + future + `"}`},
		{name: "expired", raw: `{"authorization":"Bearer abc","project":"p","expires_at":"2026-10-13T11:00:00Z"}`},
		{name: "missing expiry", raw: `{"authorization":"Bearer abc","project":"p"}`},
		{name: "unknown field", raw: `{"authorization":"Bearer abc","project":"p","expires_at":"` + future + `","api_key":"x"}`},
		{name: "trailing data", raw: `{"authorization":"Bearer abc","project":"p","expires_at":"` + future + `"} {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTokenOutput([]byte(tc.raw))
			require.Error(t, err)
		})
	}

	out, err := parseTokenOutput([]byte(`{"authorization":"Bearer abc","project":"p","expires_at":"` + future + `"}`))
	require.NoError(t, err)
	require.Equal(t, "p", out.Project)
}

// Output without a valid project is a definitive refusal, not an outage: the
// configured hooks project has no other source.
func TestParseTokenOutputRefusesMissingOrMalformedProject(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	prev := nowFunc
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = prev })
	future := now.Add(time.Hour).Format(time.RFC3339)

	for _, tc := range []struct{ name, project string }{
		{name: "missing", project: ""},
		{name: "with a space", project: `"a b"`},
		{name: "upper case", project: `"Hooks"`},
		{name: "with a slash", project: `"a/b"`},
		{name: "with a newline", project: `"a\nb"`},
		{name: "too long", project: `"` + strings.Repeat("a", 129) + `"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field := ""
			if tc.project != "" {
				field = `"project":` + tc.project + `,`
			}
			_, err := parseTokenOutput([]byte(`{"authorization":"Bearer abc",` + field + `"expires_at":"` + future + `"}`))
			require.ErrorIs(t, err, errTokenRefused)
			require.ErrorContains(t, err, "workload_identity.hooks_project_slug")
		})
	}

	out, err := parseTokenOutput([]byte(`{"authorization":"Bearer abc","project":"` + strings.Repeat("a", 128) + `","expires_at":"` + future + `"}`))
	require.NoError(t, err)
	require.Len(t, out.Project, 128)
}

// A token command that delivers no valid project refuses hooks: nothing is
// sent, no key is used in its place, nothing is spooled, and the box stays
// closed. The plugin's own project never stands in for it.
func TestTokenCommandWithoutValidProjectRefusesHooks(t *testing.T) {
	for _, tc := range []struct{ name, project string }{
		{name: "empty project", project: ""},
		{name: "malformed project", project: "Not A Slug"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeServer(t, nil)
			cfg, fc := tokenBox(t, fs.URL)
			fc.project = tc.project
			t.Setenv("SPEAKEASY_AI_HOOKS_PROJECT_SLUG", "env-project")
			stateHome := setSpoolStateHome(t)
			writeOrgSettings(cfg, true)

			requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"), "a refused box fails closed even when the org fails open")
			res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")

			require.Equal(t, 0, res.ExitCode)
			require.Equal(t, 0, fs.count(), "nothing is sent without a configured hooks project")
			require.True(t, tokenRejected(), "the refusal is recorded")
			t.Setenv("XDG_STATE_HOME", stateHome)
			require.Empty(t, listSpoolEntries(spoolDirPath()), "a refused event is not kept for replay")
			values, err := readAuthFile(authFilePath())
			if err == nil {
				require.Empty(t, values["bearer"], "no token is cached without a project")
			}
		})
	}
}

// A cached token without a valid project is never used: the command runs again.
func TestCachedTokenWithoutProjectIsNotUsed(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc := tokenBox(t, fs.URL)
	body := "kind=token\nserver_url=" + fs.URL + "\nbearer=cached-tok\nproject=\norg=org-1\nexpires_at=" +
		fc.now.Add(time.Hour).Format(time.RFC3339Nano) + "\n"
	require.NoError(t, os.WriteFile(authFilePath(), []byte(body), 0o600))

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, int32(1), fc.calls.Load())
	require.Equal(t, 1, fs.count())
	require.Equal(t, "Bearer tok-1", fs.headers[0].Get("Authorization"))
	require.Equal(t, "hooks-proj", fs.headers[0].Get("Gram-Project"))
}

// The configured hooks project from the token command is the only project
// sent; neither the plugin's project nor SPEAKEASY_AI_HOOKS_PROJECT_SLUG is.
func TestTokenProjectIgnoresPluginAndEnvProject(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc := tokenBox(t, fs.URL)
	fc.project = "configured_hooks-1"
	t.Setenv("SPEAKEASY_AI_HOOKS_PROJECT_SLUG", "env-project")

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 1, fs.count())
	require.Equal(t, "configured_hooks-1", fs.headers[0].Get("Gram-Project"))
	require.Empty(t, fs.headers[0].Get("Gram-Key"))
}

// A 403 for the project (the agent's org:hooks_ingest may not cover it) gets
// the usual single refresh and resend, then fails closed; the log does not
// claim the token is bad.
func TestProjectForbiddenFailsClosedAfterOneRetry(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusForbidden, decision{Decision: "", Reason: "", Message: "forbidden"}
	})
	cfg, fc := tokenBox(t, fs.URL)
	cfg.DebugLog = filepath.Join(t.TempDir(), "debug.log")

	requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"), "a forbidden project fails closed")

	require.Equal(t, 2, fs.count(), "one refresh and resend, then stop")
	require.Equal(t, int32(1), fc.refresh.Load())
	require.True(t, tokenRejected())
	requireNoKeySent(t, fs)
	log, err := os.ReadFile(cfg.DebugLog)
	require.NoError(t, err)
	require.Contains(t, string(log), `hooks refused for project "hooks-proj"`)
	require.Contains(t, string(log), "may not send hooks there")
}

func TestTokenCacheForAnotherDeploymentIsIgnored(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc := tokenBox(t, fs.URL)
	body := "kind=token\nserver_url=https://other.example\nbearer=other-tok\nproject=p\norg=org-1\nexpires_at=" +
		fc.now.Add(time.Hour).Format(time.RFC3339Nano) + "\n"
	require.NoError(t, os.WriteFile(authFilePath(), []byte(body), 0o600))

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, int32(1), fc.calls.Load(), "a token minted for another server must not be reused")
	require.Equal(t, "Bearer tok-1", fs.headers[0].Get("Authorization"))
}

// TestTokenRefreshInOneLongRunningRelay drives one runner through several
// events, as serve mode does, and checks it refreshes in place.
func TestTokenRefreshInOneLongRunningRelay(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc := tokenBox(t, fs.URL)
	t.Setenv("SPEAKEASY_AI_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := NewRunner(cfg)
	payload := agenthookstest.Fixture(t, "claude/post_tool_use.json")

	agenthookstest.Invoke(t, runner, agenthooks.ProviderClaudeCode, payload, "--variant=cli")
	agenthookstest.Invoke(t, runner, agenthooks.ProviderClaudeCode, payload, "--variant=cli")
	fc.now = fc.now.Add(20 * time.Minute)
	agenthookstest.Invoke(t, runner, agenthooks.ProviderClaudeCode, payload, "--variant=cli")

	require.Equal(t, int32(2), fc.calls.Load())
	require.Equal(t, 3, fs.count())
	require.Equal(t, "Bearer tok-1", fs.headers[1].Get("Authorization"))
	require.Equal(t, "Bearer tok-2", fs.headers[2].Get("Authorization"))
}

func TestLoginRefusedOnTokenBox(t *testing.T) {
	cfg, _ := tokenBox(t, "https://app.example")
	cfg.BrowserLogin = true
	err := newLoginFlow(cfg).Run(context.Background(), true)
	require.ErrorContains(t, err, "hooks token")
}

func TestExecTokenCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "hooks-token")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
printf '{"authorization":"Bearer %s","project":"%s","expires_at":"2099-01-01T00:00:00Z"}' "${SPEAKEASY_AI_HOOKS_API_KEY:-none}" "${SPEAKEASY_AI_HOOKS_TOKEN_REFRESH:-0}"
`), 0o700)) // #nosec G306 -- test script must be executable
	t.Setenv("SPEAKEASY_AI_HOOKS_API_KEY", "leaked")

	out, err := execTokenCommand(context.Background(), []string{script, "hooks-token"}, true)
	require.NoError(t, err)
	require.Contains(t, string(out), `"Bearer none"`, "a hooks key must not reach the command")
	require.Contains(t, string(out), `"project":"1"`, "a forced refresh is signalled to the command")

	_, err = execTokenCommand(context.Background(), []string{"speakeasyd", "hooks-token"}, false)
	require.ErrorContains(t, err, "absolute path")

	failing := filepath.Join(dir, "failing")
	require.NoError(t, os.WriteFile(failing, []byte("#!/bin/sh\necho 'no hooks project: Bearer eyJhbGciOiJSUzI1NiJ9.secretsecretsecret' >&2\nexit 3\n"), 0o700)) // #nosec G306 -- test script must be executable
	_, err = execTokenCommand(context.Background(), []string{failing}, false)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secretsecretsecret", "stderr is sanitized")
	require.Contains(t, err.Error(), "no hooks project")

	noisy := filepath.Join(dir, "noisy")
	require.NoError(t, os.WriteFile(noisy, []byte("#!/bin/sh\nhead -c 40000 /dev/zero | tr '\\0' 'a'\n"), 0o700)) // #nosec G306 -- test script must be executable
	_, err = execTokenCommand(context.Background(), []string{noisy}, false)
	require.ErrorContains(t, err, "exceeds")
}
