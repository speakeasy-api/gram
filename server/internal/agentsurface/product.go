package agentsurface

import "strings"

// Clients are the kinds of client an agent product runs in.
const (
	ClientCLI     = "cli"
	ClientIDE     = "ide"
	ClientDesktop = "desktop"
	ClientWeb     = "web"
	ClientMobile  = "mobile"
	ClientCloud   = "cloud"
	ClientSlack   = "slack"
)

// Placement is the agent product a raw surface names and, when the name says
// so, the kind of client it ran in. Either is empty when the name does not say.
type Placement struct {
	Product string
	Client  string
}

// placements folds the raw surfaces producers report (hook sources, dialect
// surfaces and MCP clientInfo names) into a product and a client. Bare
// "claude" is absent for the reason given on bySource. claude-ai is the MCP
// client name claude.ai connectors report from Anthropic's servers, so it says
// nothing about the device. Claude Code on the web runs in a cloud sandbox, so
// its client is cloud, not web.
var placements = map[string]Placement{
	"claude-code":         {Product: "claude-code", Client: ""},
	"claude-code-cli":     {Product: "claude-code", Client: ClientCLI},
	"claude-code-web":     {Product: "claude-code", Client: ClientCloud},
	"claude-code-desktop": {Product: "claude-code", Client: ClientDesktop},
	"claude-tag":          {Product: "claude-code", Client: ClientSlack},

	"claude-chat":         {Product: "claude-chat", Client: ""},
	"claude-ai":           {Product: "claude-chat", Client: ""},
	"claude-chat-web":     {Product: "claude-chat", Client: ClientWeb},
	"claude-web":          {Product: "claude-chat", Client: ClientWeb},
	"claude-desktop":      {Product: "claude-chat", Client: ClientDesktop},
	"claude-chat-desktop": {Product: "claude-chat", Client: ClientDesktop},
	"claude-design":       {Product: "claude-design", Client: ClientWeb},

	"cowork":         {Product: "cowork", Client: ""},
	"claude-cowork":  {Product: "cowork", Client: ""},
	"cowork-desktop": {Product: "cowork", Client: ClientDesktop},

	"codex":            {Product: "codex", Client: ""},
	"codex-cli":        {Product: "codex", Client: ClientCLI},
	"codex-web":        {Product: "codex", Client: ClientCloud},
	"codex-mcp-client": {Product: "codex", Client: ""},

	"chatgpt":      {Product: "chatgpt", Client: ""},
	"chatgpt-work": {Product: "chatgpt", Client: ""},
	"openai-mcp":   {Product: "chatgpt", Client: ""},

	"cursor":        {Product: "cursor", Client: ClientIDE},
	"cursor-vscode": {Product: "cursor", Client: ClientIDE},
	"cursor-cli":    {Product: "cursor", Client: ClientCLI},

	"copilot":            {Product: "copilot", Client: ""},
	"github-copilot":     {Product: "copilot", Client: ""},
	"visual-studio-code": {Product: "copilot", Client: ClientIDE},

	"windsurf":   {Product: "windsurf", Client: ClientIDE},
	"gemini":     {Product: "gemini", Client: ""},
	"gemini-cli": {Product: "gemini", Client: ClientCLI},
	"opencode":   {Product: "opencode", Client: ClientCLI},
	"openclaw":   {Product: "openclaw", Client: ""},
	"glean":      {Product: "glean", Client: ""},
}

var compactPlacements = func() map[string]Placement {
	index := make(map[string]Placement, len(placements))
	for source, placement := range placements {
		index[strings.ReplaceAll(source, "-", "")] = placement
	}
	return index
}()

// Place folds a raw surface into its product and client. An unrecognized name
// places nowhere: the caller keeps the raw value and guesses nothing.
func Place(raw string) Placement {
	normalized := Normalize(raw)
	if placement, ok := placements[normalized]; ok {
		return placement
	}
	if placement, ok := compactPlacements[strings.ReplaceAll(normalized, "-", "")]; ok {
		return placement
	}
	return Placement{Product: "", Client: ""}
}

// ideTerminals are the terminal.type values an editor's integrated terminal
// reports. Any other stated terminal is a command line.
var ideTerminals = []string{"vscode", "cursor", "windsurf", "zed", "jetbrains", "intellij", "pycharm", "webstorm", "goland"}

// ClientForTerminal is the client a stated terminal type implies: an editor's
// terminal is the IDE, anything else is the CLI, and nothing stated is empty.
func ClientForTerminal(terminalType string) string {
	normalized := strings.ToLower(strings.TrimSpace(terminalType))
	if normalized == "" {
		return ""
	}
	for _, ide := range ideTerminals {
		if strings.Contains(normalized, ide) {
			return ClientIDE
		}
	}
	return ClientCLI
}
