package mcpconnectionattempts

import (
	"net/http"
	"regexp"
	"strings"
)

// The client families client_family can hold. New ones can be appended.
const (
	FamilyCodexCLI        = "codex_cli"
	FamilyClaudeCode      = "claude_code"
	FamilyClaudeAI        = "claude_ai"
	FamilyOpenAIConnector = "openai_connector"
	FamilyCursor          = "cursor"
	FamilyVSCode          = "vscode"
	FamilyPerplexity      = "perplexity"
	FamilyScript          = "script"
	FamilyBrowser         = "browser"
	FamilyOther           = "other"
	FamilyCopilot         = "copilot"
	FamilyZed             = "zed"
	FamilyKiro            = "kiro"
	FamilyReplit          = "replit"
	FamilyNotion          = "notion"
)

const (
	// maxUserAgentBytes bounds what is kept and read. Real client tokens sit at the start.
	maxUserAgentBytes = 256

	// maxClientVersionBytes bounds a stored version. Real ones are far shorter.
	maxClientVersionBytes = 32

	// maxForwardedForBytes keeps the end of the header, which holds the only hop we read.
	maxForwardedForBytes = 128
)

// clientFamilies is checked in order, so an editor's Electron User-Agent reads as the
// editor before its Mozilla prefix reads as a browser. A marker matches whole words only.
var clientFamilies = []struct {
	family  string
	markers []string
}{
	{FamilyClaudeCode, []string{"claude-code", "claude-cli"}},
	{FamilyClaudeAI, []string{"claude-user", "claude.ai"}},
	{FamilyCodexCLI, []string{"codex_cli_rs", "codex_cli", "codex-cli", "codex-mcp-client", "codex"}},
	{FamilyOpenAIConnector, []string{"openai-mcp", "chatgpt-user", "chatgpt"}},
	{FamilyCursor, []string{"cursor"}},
	{FamilyCopilot, []string{"copilot"}},
	{FamilyZed, []string{"zed"}},
	{FamilyKiro, []string{"kiro"}},
	{FamilyReplit, []string{"replit-agent-mcp-client", "replit"}},
	{FamilyNotion, []string{"notion-mcp-client"}},
	{FamilyVSCode, []string{"vscode", "visual studio code - insiders", "visual studio code"}},
	{FamilyPerplexity, []string{"perplexity"}},
	{FamilyScript, []string{"python-httpx", "python-requests", "aiohttp", "node-fetch", "undici", "axios", "node", "libcurl", "curl", "wget", "go-http-client", "okhttp", "java"}},
	{FamilyBrowser, []string{"mozilla"}},
}

// clientVersionShape is what a version may look like, so caller text never reaches the column.
var clientVersionShape = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}(-[a-zA-Z0-9.]{1,16})?$`)

// classifyUserAgent returns the client family, and a version only when one sits right after
// the family's own marker and has the shape of a version. A browser's Mozilla/5.0 is no version.
func classifyUserAgent(userAgent string) (family, version string) {
	userAgent = userAgent[:min(len(userAgent), maxUserAgentBytes)]
	lower := asciiLower(userAgent)
	// A link in the User-Agent names its owner, not the client: PerplexityBot's points at perplexity.ai.
	if link := strings.Index(lower, "://"); link >= 0 {
		lower = lower[:link]
	}
	for _, candidate := range clientFamilies {
		for _, marker := range candidate.markers {
			at := markerIndex(lower, marker)
			if at < 0 {
				continue
			}
			if candidate.family == FamilyBrowser {
				return candidate.family, ""
			}
			return candidate.family, versionAfter(userAgent[at+len(marker):])
		}
	}
	return FamilyOther, ""
}

// markerIndex finds marker as whole words, so "node" misses "nodeflux" and "zed" misses "authorized".
func markerIndex(lower, marker string) int {
	for from := 0; from < len(lower); {
		at := strings.Index(lower[from:], marker)
		if at < 0 {
			return -1
		}
		at += from
		end := at + len(marker)
		if (at == 0 || !isWordByte(lower[at-1])) && (end == len(lower) || !isWordByte(lower[end])) {
			return at
		}
		from = at + 1
	}
	return -1
}

func isWordByte(b byte) bool {
	return 'a' <= b && b <= 'z' || '0' <= b && b <= '9'
}

// asciiLower keeps every byte offset, so an index into its result is an index into its input.
// strings.ToLower can change a string's length.
func asciiLower(value string) string {
	lower := []byte(value)
	for i, b := range lower {
		if 'A' <= b && b <= 'Z' {
			lower[i] = b + ('a' - 'A')
		}
	}
	return string(lower)
}

func versionAfter(rest string) string {
	if !strings.HasPrefix(rest, "/") {
		return ""
	}
	rest = rest[1:]
	if end := strings.IndexAny(rest, " ;,()"); end >= 0 {
		rest = rest[:end]
	}
	if len(rest) > maxClientVersionBytes || !clientVersionShape.MatchString(rest) {
		return ""
	}
	return rest
}

// requestClient is what a row keeps from its request until the writer reads it. It is bounded
// and copied, so a row holds no large header, and it is never stored or logged.
type requestClient struct {
	present      bool
	userAgent    string
	forwardedFor string

	// reported reads what the MCP session said about itself. It runs on the writer.
	reported func() (name, version string)
}

// FromRequest keeps what the request says about its client, for the writer to read. It parses
// nothing here, so hostile headers can't affect the request. Without a request it sets nothing.
func FromRequest(r *http.Request) Option {
	if r == nil {
		return Option{apply: nil}
	}
	userAgent := strings.Clone(r.UserAgent()[:min(len(r.UserAgent()), maxUserAgentBytes)])
	forwardedFor := lastForwardedFor(r)
	return Option{apply: func(row *Row) {
		row.client.present, row.client.userAgent, row.client.forwardedFor = true, userAgent, forwardedFor
	}}
}

// ReportedClient names the client an MCP session reported, read first when the writer names a
// row's client. read runs on the writer, so a slow lookup costs the request nothing. It must bound
// its own time and must not touch the request. Nil sets nothing.
func ReportedClient(read func() (name, version string)) Option {
	if read == nil {
		return Option{apply: nil}
	}
	return Option{apply: func(row *Row) { row.client.reported = read }}
}

// stampClient reads a row's request on the writer: its family, version and device key.
func (r *Recorder) stampClient(row *Row) {
	client := row.client
	row.client = requestClient{present: false, userAgent: "", forwardedFor: "", reported: nil}
	if client.reported != nil {
		row.ClientFamily, row.ClientVersion = classifyReported(client.reported())
	}
	if !client.present {
		return
	}
	if row.ClientFamily == "" {
		row.ClientFamily, row.ClientVersion = classifyUserAgent(client.userAgent)
	}
	row.DeviceKey = r.deviceKeyFor(row.OrganizationID, connectionIP(client.forwardedFor))
}

// classifyReported returns no family for a name it does not know, so the User-Agent gets its turn.
// A named client's version comes only from what it reported.
func classifyReported(name, version string) (string, string) {
	family, inName := classifyUserAgent(name)
	if family == FamilyOther {
		return "", ""
	}
	if len(version) <= maxClientVersionBytes && clientVersionShape.MatchString(version) {
		return family, version
	}
	return family, inName
}
