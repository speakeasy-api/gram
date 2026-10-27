package relay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

// workloadConfig is a plugin config for a sandbox whose egress proxy attaches
// the workload's token, on a machine that holds no key of its own.
func workloadConfig(t *testing.T, serverURL string) Config {
	t.Helper()
	t.Setenv("SPEAKEASY_AI_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("SPEAKEASY_AI_HOOKS_DISABLE_LOCAL_AUTH", "1")
	t.Setenv("SPEAKEASY_AI_HOOKS_API_KEY", "")
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	t.Setenv("SPEAKEASY_AI_HOOKS_WORKLOAD_AUTH", "")
	t.Setenv("GRAM_HOOKS_WORKLOAD_AUTH", "")
	return Config{ServerURL: serverURL, SiteURL: "", ProjectSlug: "default", OrgID: "org-1", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: "", WorkloadAuth: true}
}

// A workload sandbox with no key sends its events anyway, naming its project
// and no Gram-Key, so the proxy's Authorization is the only credential.
func TestWorkloadAuthSendsWithoutAKey(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := workloadConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
	require.Equal(t, 1, fs.count(), "a workload sandbox relays without a key")
	_, sentKey := fs.headers[0]["Gram-Key"]
	require.False(t, sentKey, "no Gram-Key header is sent")
	require.Equal(t, "default", fs.headers[0].Get("Gram-Project"))
	require.NotEmpty(t, fs.headers[0].Get("Idempotency-Key"))
	require.NoFileExists(t, authFilePath(), "nothing is cached for a workload")
}

// A workload deny is enforced like any other.
func TestWorkloadAuthEnforcesDeny(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked by policy X"}
	})
	cfg := workloadConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "blocked by policy X")
}

// A key the plugin still carries outranks the keyless workload credential, so
// events keep authenticating if the proxy attaches nothing. Where the proxy
// does attach a token, the server reads it before the key.
func TestWorkloadAuthKeepsTheOrgKey(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := workloadConfig(t, fs.URL)
	cfg.HooksAPIKey = "shared-org-key"

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count())
	require.Equal(t, "shared-org-key", fs.headers[0].Get("Gram-Key"))
}

// A refused workload fails closed and names the platform connection, since no
// local sign-in can fix it. The machine's own credential state is untouched.
func TestWorkloadAuthRejectionFailsClosed(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: "unauthorized"}
	})
	cfg := workloadConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "workload identity")
	require.NoFileExists(t, authFilePath()+".reauth-needed", "a workload rejection does not ask for a sign-in")
	require.False(t, authEstablished())
}

// Without the setting, a machine with no key still sends nothing.
func TestWorkloadAuthOffKeepsNeverAuthedSilent(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := workloadConfig(t, fs.URL)
	cfg.WorkloadAuth = false

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 0, fs.count(), "no events may leak before authentication")
}

// The setting is read from the plugin's speakeasy.json and from the
// environment, which can only turn it on.
func TestWorkloadAuthIsReadFromConfigAndEnvironment(t *testing.T) {
	t.Setenv("SPEAKEASY_AI_HOOKS_WORKLOAD_AUTH", "")
	t.Setenv("GRAM_HOOKS_WORKLOAD_AUTH", "")
	path := filepath.Join(t.TempDir(), "speakeasy.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"server_url":"https://gram.test","project":"default","workload_auth":true}`), 0o600))
	empty := Config{ServerURL: "", SiteURL: "", ProjectSlug: "", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: "", WorkloadAuth: false}

	fromFile, _ := SplitInlineFlags(empty, []string{"--config=" + path})
	require.True(t, LoadConfig(fromFile).WorkloadAuth)

	require.False(t, LoadConfig(empty).WorkloadAuth)
	t.Setenv("SPEAKEASY_AI_HOOKS_WORKLOAD_AUTH", "1")
	require.True(t, LoadConfig(empty).WorkloadAuth)
	t.Setenv("SPEAKEASY_AI_HOOKS_WORKLOAD_AUTH", "false")
	require.True(t, LoadConfig(fromFile).WorkloadAuth, "the environment cannot turn off the plugin's setting")
}

// A spooled entry from a workload sandbox replays under the workload
// credential, routed to the project it was recorded for.
func TestWorkloadAuthDrainReplaysWithoutAKey(t *testing.T) {
	cfg := workloadConfig(t, "https://gram.test")
	path := filepath.Join(t.TempDir(), "speakeasy.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"server_url":"https://gram.test","project":"default","org":"org-1","workload_auth":true}`), 0o600))
	entry := spoolEntry{V: spoolEntryVersion, IdempotencyKey: "idem-1", ServerURL: cfg.ServerURL, OrgID: cfg.OrgID, ProjectSlug: "recorded-project", ConfigPath: path, SpooledAt: time.Now(), Envelope: components.IngestRequestBody{}}

	a := resolveDrainAuth(entry, drainAuthKey(entry), map[string]drainAuth{})

	require.True(t, a.ok)
	require.Equal(t, credWorkload, a.c.Source)
	require.Empty(t, a.c.APIKey)
	require.Equal(t, "recorded-project", a.c.Project)
}

// The skill content upload accepts only API keys, so a workload credential
// skips it instead of failing the delivery or wedging a spooled entry.
func TestWorkloadAuthSkipsSkillContentUpload(t *testing.T) {
	content := "---\nname: demo\n---\n"
	digest := sha256.Sum256([]byte(content))
	rawSHA256 := hex.EncodeToString(digest[:])
	res := ingestResult{statusCode: http.StatusOK, decision: decision{Decision: "allow", Reason: "", Message: ""}, authRejected: false, failOpen: nil, skillCapture: &skillCapture{rawSHA256: rawSHA256, contentRequired: true}, blockEffect: nil, cause: causeNone, causeDetail: ""}
	skill := &resolvedSkill{name: "demo", rawSHA256: rawSHA256, content: content, captureReady: true}
	c := creds{ServerURL: "https://gram.test", APIKey: "", Project: "default", Email: "", Org: "org-1", Source: credWorkload}

	_, requested, err := requestedSkillUploadTask(c, res, skill)

	require.NoError(t, err)
	require.False(t, requested)
}

// A rendered plugin carries the setting in its speakeasy.json.
func TestWorkloadAuthIsWrittenToThePluginConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeConfigFile(dir, PluginConfig{ServerURL: "https://gram.test", SiteURL: "", ProjectSlug: "default", OrgID: "org-1", HooksAPIKey: "", BrowserLogin: false, BinaryPath: "/tmp/speakeasy-hooks", WorkloadAuth: true}))

	raw, err := os.ReadFile(filepath.Join(dir, configFileName))
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.Equal(t, true, doc["workload_auth"])
}
