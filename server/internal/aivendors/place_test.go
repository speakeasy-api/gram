package aivendors_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

func TestPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		product string
		client  aivendors.Client
		ok      bool
	}{
		{name: "it places a hook source with its client", raw: "claude-code-desktop", product: "claude-code", client: aivendors.ClientDesktop, ok: true},
		{name: "it places claude code on the web in the cloud", raw: "claude-code-web", product: "claude-code", client: aivendors.ClientCloud, ok: true},
		{name: "it places claude in slack", raw: "claude-tag", product: "claude-code", client: aivendors.ClientSlack, ok: true},
		{name: "it leaves the client unknown when the name does not say", raw: "claude-code", product: "claude-code", client: aivendors.ClientUnknown, ok: true},
		{name: "it places claude chat on the claude product", raw: "claude-chat-web", product: "claude", client: aivendors.ClientWeb, ok: true},
		{name: "it places cowork on its own product", raw: "cowork-desktop", product: "cowork", client: aivendors.ClientDesktop, ok: true},
		{name: "it places an MCP clientInfo name without a client", raw: "claude-ai", product: "claude-code", client: aivendors.ClientUnknown, ok: true},
		{name: "it prefers a surface over a clientInfo name", raw: "codex-cli", product: "codex", client: aivendors.ClientCLI, ok: true},
		{name: "it places a spaced, cased spelling", raw: "  Claude Code ", product: "claude-code", client: aivendors.ClientUnknown, ok: true},
		{name: "it places an unspaced spelling", raw: "cursorvscode", product: "cursor", client: aivendors.ClientIDE, ok: true},
		{name: "it places an underscored spelling", raw: "claude_code_cli", product: "claude-code", client: aivendors.ClientCLI, ok: true},
		{name: "it keeps chatgpt apart from codex", raw: "chatgpt", product: "chatgpt", client: aivendors.ClientUnknown, ok: true},
		{name: "it places gemini on gemini cli", raw: "gemini", product: "gemini-cli", client: aivendors.ClientUnknown, ok: true},
		{name: "it places bare claude nowhere", raw: "claude", product: "", client: aivendors.ClientUnknown, ok: false},
		{name: "it places an unknown name nowhere", raw: "some-new-agent", product: "", client: aivendors.ClientUnknown, ok: false},
		{name: "it places an empty name nowhere", raw: " ", product: "", client: aivendors.ClientUnknown, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			product, client, ok := aivendors.Place(tt.raw)
			require.Equal(t, tt.product, product, "product")
			require.Equal(t, tt.client, client, "client")
			require.Equal(t, tt.ok, ok, "placed")
		})
	}
}

func TestEveryReportedNamePlacesItsOwnProduct(t *testing.T) {
	t.Parallel()

	for _, product := range aivendors.Products() {
		for _, surface := range product.Surfaces {
			placed, client, ok := aivendors.Place(surface.Name)
			require.True(t, ok, "surface %q of %q", surface.Name, product.ID)
			require.Equal(t, product.ID, placed, "surface %q", surface.Name)
			require.Equal(t, surface.Client, client, "surface %q", surface.Name)
		}
		for _, name := range product.ClientInfoNames {
			placed, _, ok := aivendors.Place(name)
			require.True(t, ok, "clientInfo name %q of %q", name, product.ID)
			require.Equal(t, product.ID, placed, "clientInfo name %q", name)
		}
	}
}

func TestClientForTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		terminal string
		want     aivendors.Client
	}{
		{name: "it reads an editor terminal as the IDE", terminal: "vscode", want: aivendors.ClientIDE},
		{name: "it reads a jetbrains terminal as the IDE", terminal: "JetBrains-JediTerm", want: aivendors.ClientIDE},
		{name: "it reads a bare ide terminal as the IDE", terminal: "ide", want: aivendors.ClientIDE},
		{name: "it reads a terminal containing ide as the CLI", terminal: "slide-term", want: aivendors.ClientCLI},
		{name: "it reads any other terminal as the CLI", terminal: "iTerm.app", want: aivendors.ClientCLI},
		{name: "it says nothing for no terminal", terminal: " ", want: aivendors.ClientUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, aivendors.ClientForTerminal(tt.terminal))
		})
	}
}
