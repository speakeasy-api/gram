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
)

// maxUserAgentBytes bounds what is read. Real client tokens sit at the start.
const maxUserAgentBytes = 256

// clientFamilies is checked in order, so an editor's Electron User-Agent reads as the
// editor before its Mozilla prefix reads as a browser.
var clientFamilies = []struct {
	family  string
	markers []string
}{
	{FamilyClaudeCode, []string{"claude-code", "claude-cli"}},
	{FamilyClaudeAI, []string{"claude-user", "claude.ai", "claudebot"}},
	{FamilyCodexCLI, []string{"codex_cli_rs", "codex_cli", "codex-cli", "codex"}},
	{FamilyOpenAIConnector, []string{"openai-mcp", "chatgpt-user", "chatgpt", "openai"}},
	{FamilyCursor, []string{"cursor"}},
	{FamilyVSCode, []string{"vscode", "visual studio code"}},
	{FamilyPerplexity, []string{"perplexity"}},
	{FamilyScript, []string{"python-httpx", "python-requests", "aiohttp", "node-fetch", "undici", "axios", "node", "curl", "wget", "go-http-client", "okhttp", "java"}},
	{FamilyBrowser, []string{"mozilla"}},
}

// clientVersionShape is what a version may look like, so caller text never reaches the column.
var clientVersionShape = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}(-[a-zA-Z0-9.]{1,16})?$`)

// classifyUserAgent returns the client family, and a version only when one sits right after
// the family's own marker and has the shape of a version. A browser's Mozilla/5.0 is no version.
func classifyUserAgent(userAgent string) (family, version string) {
	userAgent = userAgent[:min(len(userAgent), maxUserAgentBytes)]
	lower := strings.ToLower(userAgent)
	for _, candidate := range clientFamilies {
		for _, marker := range candidate.markers {
			at := strings.Index(lower, marker)
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

func versionAfter(rest string) string {
	if !strings.HasPrefix(rest, "/") {
		return ""
	}
	rest = rest[1:]
	if end := strings.IndexAny(rest, " ;,()"); end >= 0 {
		rest = rest[:end]
	}
	if !clientVersionShape.MatchString(rest) {
		return ""
	}
	return rest
}

// FromRequest sets what the request says about its client. Without a request it sets nothing.
func FromRequest(r *http.Request) Option {
	if r == nil {
		return Option{apply: nil}
	}
	family, version := classifyUserAgent(r.UserAgent())
	return Option{apply: func(row *Row) {
		row.ClientFamily, row.ClientVersion = family, version
	}}
}
