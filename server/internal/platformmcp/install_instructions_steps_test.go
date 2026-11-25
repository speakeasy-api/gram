package platformmcp

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientInstallStepsGiveEachClientAWorkingPath(t *testing.T) {
	t.Parallel()

	const key = "linear"
	const endpoint = "https://mcp.example.test/mcp/acme-linear"

	cursor := clientInstallSteps(OnboardingClientCursor, key, endpoint)
	link, err := url.Parse(cursor[0].URL)
	require.NoError(t, err)
	require.Equal(t, "cursor", link.Scheme)
	require.Equal(t, key, link.Query().Get("name"))
	config, err := base64.StdEncoding.DecodeString(link.Query().Get("config"))
	require.NoError(t, err)
	require.JSONEq(t, `{"url":"`+endpoint+`"}`, string(config))
	require.JSONEq(t, `{"mcpServers":{"linear":{"url":"`+endpoint+`"}}}`, cursor[1].Snippet)

	claude := clientInstallSteps(OnboardingClientClaudeCode, key, endpoint)
	require.Equal(t, "claude mcp add --transport http linear "+endpoint, claude[0].Snippet)

	codex := clientInstallSteps(OnboardingClientCodex, key, endpoint)
	require.Equal(t, "[mcp_servers.linear]\nurl = \""+endpoint+"\"", codex[0].Snippet)
	require.Equal(t, "codex mcp login linear", codex[1].Snippet)

	opencode := clientInstallSteps(OnboardingClientOpencode, key, endpoint)
	var opencodeConfig map[string]map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(opencode[0].Snippet), &opencodeConfig))
	require.Equal(t, "remote", opencodeConfig["mcp"][key]["type"])
	require.Equal(t, endpoint, opencodeConfig["mcp"][key]["url"])

	vibe := clientInstallSteps(OnboardingClientVibe, key, endpoint)
	// `vibe mcp add` is required over a hand-written config block: it also
	// writes the [mcp_servers.auth] oauth table, without which Vibe treats the
	// server as static and a Speakeasy-hosted server fails to connect.
	require.Equal(t, "vibe mcp add linear --url "+endpoint, vibe[0].Snippet)
	require.Len(t, vibe, 1, "the /mcp login step is appended by the caller, not clientInstallSteps")

	other := clientInstallSteps(OnboardingClientOther, key, endpoint)
	require.Equal(t, endpoint, other[0].URL)
	require.JSONEq(t, `{"mcpServers":{"linear":{"url":"`+endpoint+`"}}}`, other[0].Snippet)

	cowork := clientInstallSteps(OnboardingClientClaudeCowork, key, endpoint)
	require.Equal(t, endpoint, cowork[0].URL)
	require.Empty(t, cowork[0].Snippet)
}

func TestStandaloneMCPInstallInstructionsVibeLoginStep(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://mcp.example.test")
	require.NoError(t, err)
	service := &PluginsService{serverURL: serverURL}

	output, err := service.standaloneMCPInstallInstructions(t.Context(), "", OnboardingClientVibe, "Linear", "acme-linear")
	require.NoError(t, err)
	require.True(t, output.Supported)
	require.Len(t, output.Instructions, 2)
	require.Equal(t, "vibe mcp add acme-linear --url https://mcp.example.test/mcp/acme-linear", output.Instructions[0].Snippet)
	require.Equal(t, "/mcp login acme-linear", output.Instructions[1].Snippet)
}

func TestStandaloneMCPInstallInstructionsRejectUnsafeSlug(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://mcp.example.test")
	require.NoError(t, err)
	service := &PluginsService{serverURL: serverURL}

	output, err := service.standaloneMCPInstallInstructions(t.Context(), "", OnboardingClientClaudeCode, "Example", "bad;slug$(id)")
	require.NoError(t, err)
	require.False(t, output.Supported)
	require.Empty(t, output.Instructions)
	require.NotEmpty(t, output.Fallback)
}
