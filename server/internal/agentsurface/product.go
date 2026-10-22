package agentsurface

import "strings"

const (
	ClientCLI     = "cli"
	ClientIDE     = "ide"
	ClientDesktop = "desktop"
	ClientWeb     = "web"
	ClientMobile  = "mobile"
	ClientCloud   = "cloud"
	ClientSlack   = "slack"
)

// Placement is the product a raw surface names and, when it says, the client.
type Placement struct {
	Product string
	Client  string
}

var placements = map[string]Placement{
	"claude-code":         {Product: "claude-code", Client: ""},
	"claude-code-cli":     {Product: "claude-code", Client: ClientCLI},
	"claude-code-web":     {Product: "claude-code", Client: ClientCloud},
	"claude-code-desktop": {Product: "claude-code", Client: ClientDesktop},
	"claude-tag":          {Product: "claude-code", Client: ClientSlack},

	"claude-chat": {Product: "claude-chat", Client: ""},
	// claude-ai is what claude.ai connectors report from Anthropic's servers, so it names no device.
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
	"pi":         {Product: "pi", Client: ClientCLI},
}

var compactPlacements = func() map[string]Placement {
	index := make(map[string]Placement, len(placements))
	for source, placement := range placements {
		index[strings.ReplaceAll(source, "-", "")] = placement
	}
	return index
}()

// Place folds a raw surface into its product and client; an unknown name places nowhere.
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

var ideTerminals = []string{"vscode", "cursor", "windsurf", "zed", "jetbrains", "intellij", "pycharm", "webstorm", "goland"}

// ClientForTerminal reads an editor's terminal as the IDE and any other as the CLI.
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
