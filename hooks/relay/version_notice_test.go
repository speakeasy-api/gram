package relay

import (
	"net/http"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

func versionStatusServer(t *testing.T, status string) *fakeServer {
	t.Helper()
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	fs.effects = func(components.IngestRequestBody) map[string]any {
		return map[string]any{"hooks_version": map[string]any{"status": status, "latest": "0.3.46", "minimum": "0.3.0"}}
	}
	return fs
}

func TestUnsupportedVersionNoticeOncePerSession(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	cfg := authedConfig(t, versionStatusServer(t, "unsupported").URL)

	for _, provider := range []struct {
		provider agenthooks.Provider
		fixture  string
	}{
		{agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json"},
		{agenthooks.ProviderCodex, "codex/user_prompt_submit.json"},
	} {
		t.Run(string(provider.provider), func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			first := invoke(t, cfg, provider.provider, provider.fixture)
			require.Contains(t, string(first.Stdout), "no longer supported")
			require.NotContains(t, string(first.Stdout), `"decision":"block"`)

			second := invoke(t, cfg, provider.provider, provider.fixture)
			require.NotContains(t, string(second.Stdout), "no longer supported")
		})
	}
}

func TestVersionNoticeSkipsSupportedVersions(t *testing.T) {
	for _, status := range []string{"current", "behind", ""} {
		t.Run(status, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			cfg := authedConfig(t, versionStatusServer(t, status).URL)
			res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")
			require.NotContains(t, string(res.Stdout), "no longer supported")
		})
	}
}

func TestVersionNoticeNeverChangesADeny(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked by policy"}
	})
	fs.effects = func(components.IngestRequestBody) map[string]any {
		return map[string]any{"hooks_version": map[string]any{"status": "unsupported"}}
	}
	res := invoke(t, authedConfig(t, fs.URL), agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")
	require.Contains(t, string(res.Stdout), "blocked by policy")
	require.NotContains(t, string(res.Stdout), "no longer supported")
}
