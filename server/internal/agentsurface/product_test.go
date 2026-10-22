package agentsurface_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agentsurface"
)

func TestPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		product string
		client  string
	}{
		{name: "it places a hook source with its client", raw: "claude-code-desktop", product: "claude-code", client: agentsurface.ClientDesktop},
		{name: "it places claude code on the web in the cloud", raw: "claude-code-web", product: "claude-code", client: agentsurface.ClientCloud},
		{name: "it places claude in slack", raw: "claude-tag", product: "claude-code", client: agentsurface.ClientSlack},
		{name: "it leaves the client empty when the name does not say", raw: "claude-code", product: "claude-code", client: ""},
		{name: "it places the claude.ai MCP client without a device", raw: "claude-ai", product: "claude-chat", client: ""},
		{name: "it places a spaced MCP client name", raw: "Visual Studio Code", product: "copilot", client: agentsurface.ClientIDE},
		{name: "it places an unspaced spelling", raw: "cursorvscode", product: "cursor", client: agentsurface.ClientIDE},
		{name: "it places codex cloud tasks in the cloud", raw: "codex-web", product: "codex", client: agentsurface.ClientCloud},
		{name: "it keeps chatgpt apart from codex", raw: "chatgpt", product: "chatgpt", client: ""},
		{name: "it places bare claude nowhere", raw: "claude", product: "", client: ""},
		{name: "it places an unknown name nowhere", raw: "some-new-agent", product: "", client: ""},
		{name: "it places an empty name nowhere", raw: "", product: "", client: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := agentsurface.Place(tt.raw)
			require.Equal(t, tt.product, got.Product, "product")
			require.Equal(t, tt.client, got.Client, "client")
		})
	}
}

func TestClientForTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		terminal string
		want     string
	}{
		{name: "it reads an editor terminal as the IDE", terminal: "vscode", want: agentsurface.ClientIDE},
		{name: "it reads a bare ide terminal as the IDE", terminal: "ide", want: agentsurface.ClientIDE},
		{name: "it reads a jetbrains terminal as the IDE", terminal: "JetBrains-JediTerm", want: agentsurface.ClientIDE},
		{name: "it reads any other terminal as the CLI", terminal: "iTerm.app", want: agentsurface.ClientCLI},
		{name: "it says nothing for no terminal", terminal: " ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, agentsurface.ClientForTerminal(tt.terminal))
		})
	}
}
