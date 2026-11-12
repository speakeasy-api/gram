package mcpconnectionattempts

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
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
// editor before its Mozilla prefix reads as a browser. A marker matches whole words only,
// and is written with dashes because spaces, dots and underscores all read as dashes.
var clientFamilies = []struct {
	family  string
	markers []string
}{
	{FamilyClaudeCode, []string{"claude-code", "claude-cli"}},
	{FamilyClaudeAI, []string{"claude-user", "claude-ai"}},
	{FamilyCodexCLI, []string{"codex-cli-rs", "codex-cli", "codex-mcp-client", "codex"}},
	{FamilyOpenAIConnector, []string{"openai-mcp", "openai-connectors", "chatgpt-user", "chatgpt"}},
	{FamilyCursor, []string{"cursor"}},
	{FamilyCopilot, []string{"copilot"}},
	{FamilyZed, []string{"zed"}},
	{FamilyKiro, []string{"kiro"}},
	{FamilyReplit, []string{"replit-agent-mcp-client", "replit"}},
	{FamilyNotion, []string{"notion-mcp-client"}},
	{FamilyVSCode, []string{"vscode", "visual-studio-code---insiders", "visual-studio-code"}},
	{FamilyPerplexity, []string{"perplexity"}},
	{FamilyScript, []string{"python-httpx", "python-requests", "aiohttp", "node-fetch", "undici", "axios", "node", "libcurl", "curl", "wget", "go-http-client", "okhttp", "java"}},
	{FamilyBrowser, []string{"mozilla"}},
}

// productFamilies maps an aivendors product to its family. It is keyed per product, not per
// vendor, because one vendor host (claude.ai, chatgpt.com) serves several products.
var productFamilies = map[string]string{
	"claude-code":        FamilyClaudeCode,
	"claude":             FamilyClaudeAI,
	"chatgpt":            FamilyOpenAIConnector,
	"codex":              FamilyCodexCLI,
	"vscode":             FamilyVSCode,
	"zed":                FamilyZed,
	"notion":             FamilyNotion,
	"github-copilot-cli": FamilyCopilot,
}

// vendorHostedRedirect lists families whose documents redirect only to the vendor's own host,
// so a script-like User-Agent there is the vendor's backend. A localhost redirect could be any program.
var vendorHostedRedirect = map[string]bool{FamilyClaudeAI: true, FamilyOpenAIConnector: true}

// genericFamily is a family that names no client app.
func genericFamily(family string) bool {
	return family == "" || family == FamilyOther || family == FamilyScript
}

// documentFamilies is keyed by catalog entry URL, literal or pattern, as admission.CatalogPreset returns it.
var documentFamilies = func() map[string]string {
	families := make(map[string]string)
	for _, entry := range aivendors.CIMDDocuments() {
		if family, ok := productFamilies[entry.Product.ID]; ok {
			families[entry.Document.URL] = family
		}
	}
	return families
}()

// clientVersionShape is what a version may look like, so caller text never reaches the column.
var clientVersionShape = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}(-[a-zA-Z0-9.]{1,16})?$`)

// classifyUserAgent returns the client family, and a version only when one sits right after
// the family's own marker and has the shape of a version. A browser's Mozilla/5.0 is no version.
func classifyUserAgent(userAgent string) (family, version string) {
	userAgent = userAgent[:min(len(userAgent), maxUserAgentBytes)]
	lower := foldClientName(userAgent)
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

// foldClientName lowercases ASCII and turns spaces, dots and underscores into dashes, so "Claude Code"
// and "claude-code" read alike. It keeps every byte offset, so an index into its result is an index
// into its input. strings.ToLower can change a string's length.
func foldClientName(value string) string {
	folded := []byte(value)
	for i, b := range folded {
		switch {
		case 'A' <= b && b <= 'Z':
			folded[i] = b + ('a' - 'A')
		case b == ' ' || b == '.' || b == '_':
			folded[i] = '-'
		}
	}
	return string(folded)
}

func versionAfter(rest string) string {
	if !strings.HasPrefix(rest, "/") {
		return ""
	}
	rest = rest[1:]
	if end := strings.IndexAny(rest, " ;,()"); end >= 0 {
		rest = rest[:end]
	}
	if !validClientVersion(rest) {
		return ""
	}
	return rest
}

func validClientVersion(version string) bool {
	return len(version) <= maxClientVersionBytes && clientVersionShape.MatchString(version)
}

// requestClient is what a row keeps from its request until the writer reads it. It is bounded
// and copied, so a row holds no large header, and it is never stored or logged.
type requestClient struct {
	present      bool
	userAgent    string
	forwardedFor string

	// appFamily is the family the row's admitted catalog client_id names.
	appFamily string

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
// its own time and must not touch the request: a slow read stalls the single writer
// and delays other queued rows until it returns. Nil sets nothing.
func ReportedClient(read func() (name, version string)) Option {
	if read == nil {
		return Option{apply: nil}
	}
	return Option{apply: func(row *Row) { row.client.reported = read }}
}

// stampClient reads a row's request on the writer: its family, version and device key. A reported
// name comes first, then a User-Agent that names a client, then the catalog client_id.
func (r *Recorder) stampClient(row *Row) {
	client := row.client
	row.client = requestClient{present: false, userAgent: "", forwardedFor: "", appFamily: "", reported: nil}
	// reportedUnknown is a session that reported a name no family knows, with reportedVersion.
	reportedUnknown, reportedVersion := false, ""
	if client.reported != nil {
		name, version := client.reported()
		row.ClientFamily, row.ClientVersion = classifyReported(name, version)
		if name != "" && row.ClientFamily == "" {
			reportedUnknown, reportedVersion = true, version
		}
	}
	if genericFamily(row.ClientFamily) && client.present {
		// A browser User-Agent is generic too, so it can't replace a name the session reported.
		family, version := classifyUserAgent(client.userAgent)
		if row.ClientFamily == "" || !genericFamily(family) && family != FamilyBrowser {
			row.ClientFamily, row.ClientVersion = family, version
		}
	}
	// An admitted catalog client_id beats a runtime User-Agent, whose version isn't the app's.
	if client.appFamily != "" && (row.ClientFamily == "" || row.ClientFamily == FamilyOther ||
		row.ClientFamily == FamilyScript && vendorHostedRedirect[client.appFamily]) {
		switch {
		case reportedUnknown && !vendorHostedRedirect[client.appFamily]:
			// Any local program can present this client_id, and this one named itself something else.
			row.ClientFamily, row.ClientVersion = FamilyOther, ""
		case validClientVersion(reportedVersion):
			row.ClientFamily, row.ClientVersion = client.appFamily, reportedVersion
		default:
			row.ClientFamily, row.ClientVersion = client.appFamily, ""
		}
	}
	if !client.present {
		return
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
	if validClientVersion(version) {
		return family, version
	}
	return family, inName
}
