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

var ideTerminals = []string{"vscode", "cursor", "windsurf", "zed", "jetbrains", "intellij", "pycharm", "webstorm", "goland"}

// ClientFromTerminal reads an editor's terminal as the IDE and any other as the CLI.
func ClientFromTerminal(terminalType string) Client {
	normalized := strings.ToLower(strings.TrimSpace(terminalType))
	if normalized == "" {
		return ClientUnknown
	}
	if normalized == string(ClientIDE) {
		return ClientIDE
	}
	for _, ide := range ideTerminals {
		if strings.Contains(normalized, ide) {
			return ClientIDE
		}
	}
	return ClientCLI
}
