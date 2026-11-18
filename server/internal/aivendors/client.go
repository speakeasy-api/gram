package aivendors

import "strings"

// Client is the kind of client a product runs in.
type Client string

const (
	// ClientUnknown is a name or signal that says nothing about the client.
	ClientUnknown Client = ""

	ClientCLI     Client = "cli"
	ClientIDE     Client = "ide"
	ClientDesktop Client = "desktop"
	ClientWeb     Client = "web"
	ClientMobile  Client = "mobile"
	ClientCloud   Client = "cloud"
	ClientSlack   Client = "slack"
)

// Terminal types that are not a person at a command line.
const (
	// terminalNonInteractive is a headless run: the SDK, a pipe or the
	// desktop app's background session.
	terminalNonInteractive = "non-interactive"

	// terminalClaudeDesktop is Claude Code running inside the desktop app.
	terminalClaudeDesktop = "claude-desktop"
)

// ideTerminals are the names editors give their integrated terminals,
// JetBrains IDEs each under their own.
var ideTerminals = []string{
	"vscode", "cursor", "windsurf", "zed", "fleet",
	"jetbrains", "intellij", "pycharm", "webstorm", "goland", "phpstorm", "rider",
	"clion", "rubymine", "datagrip", "dataspell", "studio",
}

// ClientFromTerminal reads an editor's terminal as the IDE, the desktop app's
// as the desktop, a headless run as nothing, and any other as the CLI.
func ClientFromTerminal(terminalType string) Client {
	normalized := strings.ToLower(strings.TrimSpace(terminalType))
	switch normalized {
	case "", terminalNonInteractive:
		return ClientUnknown
	case terminalClaudeDesktop:
		return ClientDesktop
	case string(ClientIDE):
		return ClientIDE
	}
	for _, ide := range ideTerminals {
		if strings.Contains(normalized, ide) {
			return ClientIDE
		}
	}
	return ClientCLI
}
