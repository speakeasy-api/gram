package relay

import (
	"bytes"
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

// shrinkRetryBudget collapses the SDK's 5xx backoff budget so server-error
// tests don't pay the production 30s retry wall clock.
func shrinkRetryBudget(t *testing.T) {
	t.Helper()
	prev := retryMaxElapsedMS
	retryMaxElapsedMS = 1
	t.Cleanup(func() { retryMaxElapsedMS = prev })
}

func orgSettingsEffects(failOpen bool) func(components.IngestRequestBody) map[string]any {
	return func(components.IngestRequestBody) map[string]any {
		return map[string]any{"org_settings": map[string]any{"fail_open": failOpen}}
	}
}

// seedOrgSettings writes the cache file directly with a controlled age so
// tests can exercise the max-age cutoff and the periodic refresh.
func seedOrgSettings(t *testing.T, cfg Config, failOpen bool, age time.Duration) {
	t.Helper()
	body, err := json.Marshal(orgSettings{
		ServerURL: cfg.ServerURL,
		Org:       cfg.OrgID,
		FailOpen:  failOpen,
		UpdatedAt: time.Now().UTC().Add(-age),
	})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(orgSettingsPath()), 0o700))
	require.NoError(t, os.WriteFile(orgSettingsPath(), body, 0o600))
}

// TestIngestEffectsWriteOrgSettingsCache pins the sync path: a successful
// exchange carrying org_settings persists the fail-open value next to the
// credential cache, and a later value overwrites it.
func TestIngestEffectsWriteOrgSettingsCache(t *testing.T) {
	fs := newFakeServer(t, nil)
	fs.effects = orgSettingsEffects(true)
	cfg := authedConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, 0, res.ExitCode)

	got, ok := readOrgSettings(cfg)
	require.True(t, ok, "a server-confirmed setting must be cached")
	require.True(t, got.FailOpen)
	require.FileExists(t, authFilePath()+".org-settings.json")

	fs.effects = orgSettingsEffects(false)
	res = invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, 0, res.ExitCode)

	got, ok = readOrgSettings(cfg)
	require.True(t, ok)
	require.False(t, got.FailOpen, "a stale fail-open value must ratchet back on the next success")
}

// TestIngestDenyStillRefreshesOrgSettings: the sync rides every authenticated
// response, including denies.
func TestIngestDenyStillRefreshesOrgSettings(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked"}
	})
	fs.effects = orgSettingsEffects(true)
	cfg := authedConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)

	got, ok := readOrgSettings(cfg)
	require.True(t, ok)
	require.True(t, got.FailOpen)
}

// TestIngestWithoutEffectsLeavesOrgSettings: a response carrying no
// org_settings must not disturb the cached value.
func TestIngestWithoutEffectsLeavesOrgSettings(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	writeOrgSettings(cfg, true)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, 0, res.ExitCode)

	got, ok := readOrgSettings(cfg)
	require.True(t, ok)
	require.True(t, got.FailOpen)
}

// TestServerErrorFailsOpenWithCachedSetting is the feature's core case: a 5xx
// with a cached fail-open choice lets the gating event through.
func TestServerErrorFailsOpenWithCachedSetting(t *testing.T) {
	shrinkRetryBudget(t)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusServiceUnavailable, decision{Decision: "", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	writeOrgSettings(cfg, true)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
}

// TestServerErrorBlocksWithoutCachedSetting: absent any cached choice the
// default posture stays fail closed on 5xx.
func TestServerErrorBlocksWithoutCachedSetting(t *testing.T) {
	shrinkRetryBudget(t)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusServiceUnavailable, decision{Decision: "", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
}

// TestServerErrorBlocksWhenCachedFailClosed: an explicit fail-closed choice
// blocks on 5xx like the default.
func TestServerErrorBlocksWhenCachedFailClosed(t *testing.T) {
	shrinkRetryBudget(t)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusInternalServerError, decision{Decision: "", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	writeOrgSettings(cfg, false)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
}

// TestUnreachableFailsOpenWithCachedSetting covers the actual outage shape:
// the server is gone entirely (connection refused, statusCode 0).
func TestUnreachableFailsOpenWithCachedSetting(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	writeOrgSettings(cfg, true)
	fs.Close()

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
}

// TestUnreachableBlocksWithoutCachedSetting mirrors the default posture for a
// dead server.
func TestUnreachableBlocksWithoutCachedSetting(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	fs.Close()

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
}

// TestClientErrorStaysClosedDespiteFailOpen: a definitive 4xx means the server
// is up and answering — the downtime toggle must not weaken that posture.
func TestClientErrorStaysClosedDespiteFailOpen(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusBadRequest, decision{Decision: "", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	writeOrgSettings(cfg, true)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "HTTP 400")
}

// TestEnvKeyRejectionStaysClosedDespiteFailOpen: a 401 on the explicit env key
// reaches the same fallthrough as transport failures, and must keep failing
// closed — a broken credential is not an outage.
func TestEnvKeyRejectionStaysClosedDespiteFailOpen(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	writeOrgSettings(cfg, true)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "GRAM_HOOKS_API_KEY")
}

// TestCachedKeyRejectionRatchetUnchangedByFailOpen: the 401 credential ratchet
// (forget key, mark reauth, fail closed) is untouched by a cached fail-open
// choice.
func TestCachedKeyRejectionRatchetUnchangedByFailOpen(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: ""}
	})
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url="+fs.URL+"\napi_key=revoked-key\nproject=default\n"), 0o600))
	require.NoError(t, os.WriteFile(authFile+".established", []byte{}, 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	writeOrgSettings(cfg, true)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "a rejected credential must fail closed regardless of fail-open")
	_, statErr := os.Stat(authFile)
	require.True(t, os.IsNotExist(statErr), "the rejected cached key must still be forgotten")
	require.FileExists(t, authFile+".reauth-needed")
}

// TestOrgSettingsCacheScopedToServer: a value learned from one deployment must
// not govern another (dev cache must not fail production open).
func TestOrgSettingsCacheScopedToServer(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	other := cfg
	other.ServerURL = "https://other.gram.test"
	writeOrgSettings(other, true)
	fs.Close()

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "another server's cached setting must be ignored")
}

// TestOrgSettingsCacheScopedToOrg: within one server, another org's posture
// must not apply.
func TestOrgSettingsCacheScopedToOrg(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	cfg.OrgID = "org-1"
	other := cfg
	other.OrgID = "org-2"
	writeOrgSettings(other, true)
	fs.Close()

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "another org's cached setting must be ignored")
}

// TestOrgSettingsCacheRequiresExactOrgMatch: a config that does not declare an
// org must not inherit a posture recorded under some org's identity — the
// match is exact, not one-sided like the credential cache.
func TestOrgSettingsCacheRequiresExactOrgMatch(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	other := cfg
	other.OrgID = "org-1"
	writeOrgSettings(other, true)
	fs.Close()

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "an org-scoped cached setting must not apply to an org-less config")
}

// TestFutureTimestampedOrgSettingsIgnored: a future updated_at (clock rolled
// back since the write) has an unknowable age, so it must read as stale
// instead of surviving the max-age cutoff indefinitely.
func TestFutureTimestampedOrgSettingsIgnored(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	seedOrgSettings(t, cfg, true, -48*time.Hour)
	fs.Close()

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "a future-stamped cached setting must be ignored")
}

// TestForgetAuthLeavesOrgSettings: losing a credential must not flip the org's
// enforcement posture.
func TestForgetAuthLeavesOrgSettings(t *testing.T) {
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	cfg := Config{ServerURL: "https://gram.test", ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	writeOrgSettings(cfg, true)

	forgetAuth()

	got, ok := readOrgSettings(cfg)
	require.True(t, ok, "forgetting credentials must not clear the settings cache")
	require.True(t, got.FailOpen)
}

// TestStaleOrgSettingsRevertToFailClosed: a posture unconfirmed for longer
// than the max age must not govern enforcement — the org may have reversed it
// while the machine was offline.
func TestStaleOrgSettingsRevertToFailClosed(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	seedOrgSettings(t, cfg, true, orgSettingsMaxAge+time.Hour)
	fs.Close()

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "a stale cached fail-open must not apply")
}

// TestAgedOrgSettingsWithinMaxAgeStillApply pins the cutoff boundary: an
// entry just inside the max age still governs.
func TestAgedOrgSettingsWithinMaxAgeStillApply(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	seedOrgSettings(t, cfg, true, orgSettingsMaxAge-time.Hour)
	fs.Close()

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
}

// TestUnchangedOrgSettingsRefreshWhenAged: a successful exchange rewrites an
// aged entry even when the value is unchanged, so a continuously-syncing
// machine never drifts toward the max-age cutoff.
func TestUnchangedOrgSettingsRefreshWhenAged(t *testing.T) {
	fs := newFakeServer(t, nil)
	fs.effects = orgSettingsEffects(true)
	cfg := authedConfig(t, fs.URL)
	seedOrgSettings(t, cfg, true, orgSettingsRefreshAge+time.Hour)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, 0, res.ExitCode)

	got, ok := readOrgSettings(cfg)
	require.True(t, ok)
	require.True(t, got.FailOpen)
	require.Less(t, time.Since(got.UpdatedAt), time.Hour, "an aged unchanged entry must be re-stamped on a successful exchange")
}

// TestUnchangedRecentOrgSettingsSkipRewrite: the common case — a recently
// confirmed unchanged value — does not rewrite the file.
func TestUnchangedRecentOrgSettingsSkipRewrite(t *testing.T) {
	fs := newFakeServer(t, nil)
	fs.effects = orgSettingsEffects(true)
	cfg := authedConfig(t, fs.URL)
	seedOrgSettings(t, cfg, true, time.Minute)
	before, err := os.ReadFile(orgSettingsPath())
	require.NoError(t, err)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, 0, res.ExitCode)

	after, err := os.ReadFile(orgSettingsPath())
	require.NoError(t, err)
	require.Equal(t, before, after, "a recently confirmed unchanged value must skip the rewrite")
}

// TestFailOpenEnvOverride: the escape hatch works with no cache file at all.
func TestFailOpenEnvOverride(t *testing.T) {
	shrinkRetryBudget(t)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusServiceUnavailable, decision{Decision: "", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	t.Setenv("GRAM_HOOKS_FAIL_OPEN", "1")

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
}
