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
	require.Equal(t, FamilyClaudeCode, row.ClientFamily)
	require.Equal(t, "2.0.14", row.ClientVersion)
}

func TestFromRequest_NoRequestSetsNothing(t *testing.T) {
	t.Parallel()

	row, err := RowAt(Begin(testScope()), StepProviderRefresh, ResultRefreshed, true, time.Now(), FromRequest(nil))
	require.NoError(t, err)
	require.Empty(t, row.ClientFamily)
	require.Empty(t, row.ClientVersion)
}
