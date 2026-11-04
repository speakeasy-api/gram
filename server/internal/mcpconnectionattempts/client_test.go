package mcpconnectionattempts

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/testenv"
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

		{"codex-mcp-client/0.146.0", FamilyCodexCLI, "0.146.0"},
		{"copilot/1.2.3", FamilyCopilot, "1.2.3"},
		{"Zed/0.200.0 (macos; aarch64)", FamilyZed, "0.200.0"},
		{"Zed", FamilyZed, ""},
		{"Mozilla/5.0 (Macintosh) AppleWebKit/537.36 Kiro/0.5.1 Chrome/132.0 Electron/34.5", FamilyKiro, "0.5.1"},
		{"Replit-Agent-MCP-Client/1.0", FamilyReplit, "1.0"},
		{"Replit/2.0", FamilyReplit, "2.0"},
		{"notion-mcp-client/1.4.0", FamilyNotion, "1.4.0"},

		{"Visual Studio Code - Insiders/1.100.0-insider", FamilyVSCode, "1.100.0-insider"},
		{"PerplexityBot/1.0; +https://perplexity.ai/perplexitybot", FamilyOther, ""},
		{"SomeAgent/1.0 (+https://claude.ai/cursor)", FamilyOther, ""},
		{"nodeflux-agent/1.0", FamilyOther, ""},
		{"javascript-agent/1.0", FamilyOther, ""},
		{"mycodexagent/1.0", FamilyOther, ""},
		{"precursor/1.0", FamilyOther, ""},
		{"authorized-agent/1.0", FamilyOther, ""},
		{"Windsurf/1.2.0", FamilyOther, ""},
		{"Slack/4.38", FamilyOther, ""},
		{"droid/0.2.1", FamilyOther, ""},
		{"openai-python/1.50.0", FamilyOther, ""},
		{"OpenAI/Python 1.82.0", FamilyOther, ""},
		{"langchain-openai/0.3.0", FamilyOther, ""},
		{"ClaudeBot/1.0", FamilyOther, ""},
		{"Node.js/20.11.0", FamilyScript, ""},
		{"node/20.11.0", FamilyScript, "20.11.0"},
		{"Java/17.0.2", FamilyScript, "17.0.2"},
		{"libcurl/8.7.1", FamilyScript, "8.7.1"},
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

// frozenFamilies is append-only like the codes: stored rows and the rollup key use these names.
var frozenFamilies = []string{
	"codex_cli", "claude_code", "claude_ai", "openai_connector", "cursor", "vscode", "perplexity", "script", "browser", "other",
	"copilot", "zed", "kiro", "replit", "notion",
}

func knownFamilies() []string {
	families := []string{FamilyOther}
	for _, candidate := range clientFamilies {
		families = append(families, candidate.family)
	}
	return families
}

func TestClientFamilies_AreAppendOnly(t *testing.T) {
	t.Parallel()

	require.ElementsMatch(t, frozenFamilies, knownFamilies())
}

// Through the real writer, the new Codex User-Agent keeps its version in the stored columns.
func TestRecorder_StoresTheCodexMCPClientVersion(t *testing.T) {
	t.Parallel()

	conn := clickhouseConn(t)
	recorder := NewRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, logsOn)
	attempt := Begin(testScope())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "codex-mcp-client/0.146.0")

	recorder.End(t.Context(), attempt, StepInboundToken, ResultCodeIssued, FromRequest(req))
	require.NoError(t, recorder.Shutdown(t.Context()))

	var family, version string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT client_family, client_version FROM mcp_connection_events WHERE attempt_id = ?`, attempt.ID()).Scan(&family, &version))
	require.Equal(t, FamilyCodexCLI, family)
	require.Equal(t, "0.146.0", version)
}

// Through the real writer, the client's own name beats the User-Agent and keeps the device key.
func TestRecorder_StoresTheReportedClient(t *testing.T) {
	t.Parallel()

	conn := clickhouseConn(t)
	keys := func(string, string) ([]byte, error) { return []byte("key"), nil }
	recorder := NewRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, logsOn, WithDeviceKeys(keys))
	attempt := Begin(testScope())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "python-httpx/0.28.1")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")

	recorder.Once(t.Context(), testScope(), StepMCPRequest, CodeProviderServerError, FromRequest(req), ReportedClient(func() (string, string) { return "claude-code", "2.4.0" }))
	recorder.End(t.Context(), attempt, StepMCPRequest, CodeProviderServerError, ReportedClient(func() (string, string) { return "claude-code", "2.4.0" }), FromRequest(req))
	require.NoError(t, recorder.Shutdown(t.Context()))

	var family, version, device string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT client_family, client_version, device_key FROM mcp_connection_events WHERE attempt_id = ?`, attempt.ID()).Scan(&family, &version, &device))
	require.Equal(t, FamilyClaudeCode, family)
	require.Equal(t, "2.4.0", version)
	require.NotEmpty(t, device)
}

func TestReportedClient_ReadsFirstAndFallsBack(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("9", maxClientVersionBytes+1)
	for _, tc := range []struct {
		name, reported, reportedVersion, userAgent string
		family, version                            string
	}{
		{name: "name beats user agent", reported: "codex-mcp-client", reportedVersion: "0.50.0", userAgent: "python-httpx/0.28.1", family: FamilyCodexCLI, version: "0.50.0"},
		{name: "named client takes no version from the user agent", reported: "claude-code", userAgent: "claude-code/2.1.0", family: FamilyClaudeCode},
		{name: "version inside the name", reported: "claude-code/2.2.0", userAgent: "x", family: FamilyClaudeCode, version: "2.2.0"},
		{name: "junk version dropped", reported: "cursor", reportedVersion: "not a version", userAgent: "x", family: FamilyCursor},
		{name: "long version dropped", reported: "cursor", reportedVersion: long, userAgent: "x", family: FamilyCursor},
		{name: "no name uses user agent", userAgent: "claude-code/2.1.0", family: FamilyClaudeCode, version: "2.1.0"},
		{name: "unknown name uses user agent", reported: "acme-agent", reportedVersion: "9.9", userAgent: "python-httpx/0.28.1", family: FamilyScript, version: "0.28.1"},
		{name: "unknown everywhere", reported: "acme-agent", userAgent: "acme", family: FamilyOther},
		{name: "oversized name stays bounded", reported: strings.Repeat("a ", 1<<16) + "cursor", userAgent: "python-httpx/0.28.1", family: FamilyScript, version: "0.28.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("User-Agent", tc.userAgent)
			row, err := RowAt(Begin(testScope()), StepMCPRequest, CodeProviderServerError, false, time.Now(), ReportedClient(func() (string, string) { return tc.reported, tc.reportedVersion }), FromRequest(req))
			require.NoError(t, err)
			require.Empty(t, row.ClientFamily, "nothing is read on the request path")
			(&Recorder{}).stampClient(&row)
			require.Equal(t, tc.family, row.ClientFamily)
			require.Equal(t, tc.version, row.ClientVersion)
		})
	}
}

// A row with no request can still be named, and a nil reader is nothing.
func TestReportedClient_WithoutARequest(t *testing.T) {
	t.Parallel()

	row, err := RowAt(Begin(testScope()), StepMCPRequest, CodeProviderServerError, false, time.Now(), ReportedClient(func() (string, string) { return "cursor", "" }))
	require.NoError(t, err)
	(&Recorder{}).stampClient(&row)
	require.Equal(t, FamilyCursor, row.ClientFamily)
	require.Empty(t, row.DeviceKey)

	row, err = RowAt(Begin(testScope()), StepMCPRequest, CodeProviderServerError, false, time.Now(), ReportedClient(nil))
	require.NoError(t, err)
	(&Recorder{}).stampClient(&row)
	require.Empty(t, row.ClientFamily)
}

// A burst the limiter holds back must not read anything on its way out.
func TestReportedClient_HeldBackRowsReadNothing(t *testing.T) {
	t.Parallel()

	var reads atomic.Int32
	var written atomic.Int32
	recorder := NewCapturing(func(Row) { written.Add(1) }, logsOn)
	recorder.limiter = newOnceLimiter()
	for range 50 {
		recorder.Once(t.Context(), testScope(), StepMCPRequest, CodeProviderServerError, ReportedClient(func() (string, string) {
			reads.Add(1)
			return "cursor", ""
		}))
	}
	require.Equal(t, written.Load(), reads.Load())
	require.Less(t, reads.Load(), int32(50))
}

// A row held back for its late count keeps its reader and is named when it is written.
func TestReportedClient_LateRowIsNamed(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	row, err := RowAt(Begin(testScope()), StepMCPRequest, CodeProviderServerError, true, time.Now(), ReportedClient(func() (string, string) { return "cursor", "1.2" }))
	require.NoError(t, err)
	limiter.holdBack(row)
	rows := limiter.pendingRows(time.Now(), true)
	require.Len(t, rows, 1)
	(&Recorder{}).stampClient(&rows[0])
	require.Equal(t, FamilyCursor, rows[0].ClientFamily)
	require.Equal(t, "1.2", rows[0].ClientVersion)
}

func BenchmarkReportedClientOption(b *testing.B) {
	read := func() (string, string) { return "cursor", "" }
	for b.Loop() {
		_ = ReportedClient(read)
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

	require.Contains(t, knownFamilies(), family)
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
