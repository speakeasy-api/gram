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

	// Inferred from product names in Figma's client catalog, not seen in Speakeasy traffic.
	FamilyXcode         = "xcode"
	FamilyGrok          = "grok"
	FamilyAntigravity   = "antigravity"
	FamilySlack         = "slack"
	FamilyWindsurf      = "windsurf"
	FamilyWarp          = "warp"
	FamilyFactoryDroid  = "factory_droid"
	FamilyAugment       = "augment"
	FamilyRovo          = "rovo"
	FamilyAndroidStudio = "android_studio"
	FamilyAmazonQ       = "amazon_q"
	FamilyOpenHands     = "openhands"
	FamilyServiceNow    = "servicenow"
	FamilyDevin         = "devin"
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

	// Inferred from product names in Figma's client catalog, not seen in Speakeasy traffic.
	{FamilyXcode, []string{"xcode"}},
	{FamilyGrok, []string{"grok"}},
	{FamilyAntigravity, []string{"antigravity"}},
	{FamilySlack, []string{"slack"}},
	{FamilyWindsurf, []string{"windsurf"}},
	{FamilyWarp, []string{"warp"}},
	{FamilyFactoryDroid, []string{"droid"}},
	{FamilyAugment, []string{"augment"}},
	{FamilyRovo, []string{"rovo"}},
	{FamilyAndroidStudio, []string{"android studio", "androidstudio"}},
	{FamilyAmazonQ, []string{"amazon-q", "amazonq"}},
	{FamilyOpenHands, []string{"openhands"}},
	{FamilyServiceNow, []string{"servicenow"}},
	{FamilyDevin, []string{"devin"}},

	{FamilyVSCode, []string{"vscode", "visual studio code"}},
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
}

// FromRequest keeps what the request says about its client, for the writer to read. It parses
// nothing here, so hostile headers can't affect the request. Without a request it sets nothing.
func FromRequest(r *http.Request) Option {
	if r == nil {
		return Option{apply: nil}
	}
	userAgent := r.UserAgent()
	client := requestClient{
		present:      true,
		userAgent:    strings.Clone(userAgent[:min(len(userAgent), maxUserAgentBytes)]),
		forwardedFor: lastForwardedFor(r),
	}
	return Option{apply: func(row *Row) { row.client = client }}
}

// stampClient reads a row's request on the writer: its family, version and device key.
func (r *Recorder) stampClient(row *Row) {
	client := row.client
	row.client = requestClient{present: false, userAgent: "", forwardedFor: ""}
	if !client.present {
		return
	}
	row.ClientFamily, row.ClientVersion = classifyUserAgent(client.userAgent)
	row.DeviceKey = r.deviceKeyFor(row.OrganizationID, connectionIP(client.forwardedFor))
}
