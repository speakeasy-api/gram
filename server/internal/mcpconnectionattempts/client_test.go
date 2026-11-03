package mcpconnectionattempts

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClassifyUserAgent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		userAgent string
		family    string
		version   string
	}{
		{"codex_cli_rs/0.77.0 (Mac OS 15.1.0; arm64) iTerm.app/3.5.0", FamilyCodexCLI, "0.77.0"},
		{"codex_cli/0.77.0", FamilyCodexCLI, "0.77.0"},
		{"codex-cli/1.2.3-beta.1", FamilyCodexCLI, "1.2.3-beta.1"},
		{"claude-code/2.0.14 (cli)", FamilyClaudeCode, "2.0.14"},
		{"claude-cli/1.0.128 (external, cli)", FamilyClaudeCode, "1.0.128"},
		{"Claude-User (+https://support.anthropic.com/)", FamilyClaudeAI, ""},
		{"Claude-User/1.0; +Claude-User@anthropic.com", FamilyClaudeAI, "1.0"},
		{"openai-mcp/1.0.0", FamilyOpenAIConnector, "1.0.0"},
		{"Mozilla/5.0 AppleWebKit/537.36; compatible; ChatGPT-User/1.0; +https://openai.com/bot", FamilyOpenAIConnector, "1.0"},
		{"Cursor/2.0.77 (darwin arm64)", FamilyCursor, "2.0.77"},
		{"Mozilla/5.0 (Macintosh) AppleWebKit/537.36 Cursor/1.7.3 Chrome/132.0 Electron/34.5", FamilyCursor, "1.7.3"},
		{"cursor-vscode/1.0.0", FamilyCursor, ""},
		{"Visual Studio Code/1.105.1", FamilyVSCode, "1.105.1"},
		{"vscode/1.99.0", FamilyVSCode, "1.99.0"},
		{"Perplexity-User/1.0; +https://perplexity.ai/perplexity-user", FamilyPerplexity, ""},
		{"python-httpx/0.28.1", FamilyScript, "0.28.1"},
		{"python-requests/2.32.3", FamilyScript, "2.32.3"},
		{"node", FamilyScript, ""},
		{"node-fetch/1.0 (+https://github.com/bitinn/node-fetch)", FamilyScript, "1.0"},
		{"curl/8.7.1", FamilyScript, "8.7.1"},
		{"Go-http-client/1.1", FamilyScript, "1.1"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/141.0.0.0 Safari/537.36", FamilyBrowser, ""},
		{"SomeCompanyAgent/9.9", FamilyOther, ""},
		{"", FamilyOther, ""},
		{"curl/not-a-version;drop", FamilyScript, ""},
		{"curl/1.2.3-this-suffix-is-far-too-long-to-keep", FamilyScript, ""},
		{"curl/token=abc123", FamilyScript, ""},
		{"cursor/person@example.com", FamilyCursor, ""},
		{strings.Repeat("x", 300) + "curl/8.0", FamilyOther, ""},
	}
	for _, tc := range cases {
		t.Run(tc.userAgent, func(t *testing.T) {
			t.Parallel()

			family, version := classifyUserAgent(tc.userAgent)
			require.Equal(t, tc.family, family)
			require.Equal(t, tc.version, version)
		})
	}
}

func TestFromRequest_SetsTheClient(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "claude-code/2.0.14 (cli)")
	row, err := RowAt(Begin(testScope()), StepMCPRequest, ResultAllowed, false, time.Now(), FromRequest(req))
	require.NoError(t, err)
	require.Empty(t, row.ClientFamily, "nothing is parsed on the request path")
	(&Recorder{}).stampClient(&row)
	require.Equal(t, FamilyClaudeCode, row.ClientFamily)
	require.Equal(t, "2.0.14", row.ClientVersion)
}

func TestFromRequest_NoRequestSetsNothing(t *testing.T) {
	t.Parallel()

	row, err := RowAt(Begin(testScope()), StepProviderRefresh, ResultRefreshed, true, time.Now(), FromRequest(nil))
	require.NoError(t, err)
	(&Recorder{}).stampClient(&row)
	require.Empty(t, row.ClientFamily)
	require.Empty(t, row.ClientVersion)
}

// hostileUserAgents are values a caller can send. Ⱥ grows when lowercased, which once moved offsets.
var hostileUserAgents = []string{
	"Ⱥcursor",
	"ȺȺȺȺcursor/1.0",
	"ȺCURSOR/1.0",
	"cursorȺ/1.0",
	"\xff\xfe\xfdcursor/1.0",
	"cursor/\xff\xfe",
	"cursor/1.0\xff",
	"\x00\x01\x7fcursor/1\x00",
	"curl/1.0\r\nX-Injected: yes",
	"cursor/" + strings.Repeat("1", 1000),
	"cursor/" + strings.Repeat("1.", 100),
	strings.Repeat("Ⱥ", 1000) + "cursor/1.0",
	strings.Repeat("\xff", 10_000),
	"cursor",
	"cursor/",
	"/",
	"",
}

func requireBoundedClient(t *testing.T, family, version string) {
	t.Helper()

	require.Contains(t, []string{FamilyCodexCLI, FamilyClaudeCode, FamilyClaudeAI, FamilyOpenAIConnector, FamilyCursor, FamilyVSCode, FamilyPerplexity, FamilyScript, FamilyBrowser, FamilyOther}, family)
	require.LessOrEqual(t, len(version), maxClientVersionBytes)
	if version != "" {
		require.Regexp(t, clientVersionShape, version)
	}
}

func TestClassifyUserAgent_HostileValues(t *testing.T) {
	t.Parallel()

	for _, userAgent := range hostileUserAgents {
		family, version := classifyUserAgent(userAgent)
		requireBoundedClient(t, family, version)
	}

	family, version := classifyUserAgent("ȺCURSOR/1.0")
	require.Equal(t, FamilyCursor, family)
	require.Equal(t, "1.0", version, "the offset still lands right after the marker")
}

func FuzzClassifyUserAgent(f *testing.F) {
	for _, userAgent := range hostileUserAgents {
		f.Add(userAgent)
	}
	f.Fuzz(func(t *testing.T, userAgent string) {
		family, version := classifyUserAgent(userAgent)
		requireBoundedClient(t, family, version)
	})
}

// Hostile headers reach the writer and the row, and neither the request nor the writer breaks.
func TestFromRequest_HostileHeadersNeverPanic(t *testing.T) {
	t.Parallel()

	recorder := &Recorder{deviceKeys: testDeviceKeys(testDeviceSecret)}
	for _, value := range hostileUserAgents {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header["User-Agent"] = []string{value}
		req.Header["X-Forwarded-For"] = []string{value}
		row, err := RowAt(Begin(testScope()), StepMCPRequest, ResultAllowed, false, time.Now(), FromRequest(req))
		require.NoError(t, err)
		require.LessOrEqual(t, len(row.client.userAgent), maxUserAgentBytes)
		require.LessOrEqual(t, len(row.client.forwardedFor), maxForwardedForBytes)

		recorder.stampClient(&row)
		requireBoundedClient(t, row.ClientFamily, row.ClientVersion)
		require.Empty(t, row.DeviceKey, "none of these is an address")
	}
}

// The other normalizers lowercase too, and must hold up to the same values.
func TestNormalizers_HostileValues(t *testing.T) {
	t.Parallel()

	for _, value := range hostileUserAgents {
		for _, input := range []string{value, "https://" + value, "https://" + value + "/path"} {
			require.LessOrEqual(t, len(NormalizeProviderHost(input)), maxProviderHostBytes)
			require.LessOrEqual(t, len(AppOf(input).String()), maxProviderHostBytes)
		}
	}
}

func FuzzNormalizers(f *testing.F) {
	for _, value := range hostileUserAgents {
		f.Add(value)
		f.Add("https://" + value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		require.LessOrEqual(t, len(NormalizeProviderHost(value)), maxProviderHostBytes)
		require.LessOrEqual(t, len(AppOf(value).String()), maxProviderHostBytes)
	})
}
