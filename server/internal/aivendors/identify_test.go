package aivendors_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

func TestIdentify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		product string
		client  aivendors.Client
		ok      bool
	}{
		{name: "it identifies a hook source with its client", raw: "claude-code-desktop", product: "claude-code", client: aivendors.ClientDesktop, ok: true},
		{name: "it identifies claude code on the web in the cloud", raw: "claude-code-web", product: "claude-code", client: aivendors.ClientCloud, ok: true},
		{name: "it identifies claude in slack", raw: "claude-tag", product: "claude-code", client: aivendors.ClientSlack, ok: true},
		{name: "it leaves the client unknown when the name does not say", raw: "claude-code", product: "claude-code", client: aivendors.ClientUnknown, ok: true},
		{name: "it identifies claude chat as the claude product", raw: "claude-chat-web", product: "claude", client: aivendors.ClientWeb, ok: true},
		{name: "it identifies cowork as its own product", raw: "cowork-desktop", product: "cowork", client: aivendors.ClientDesktop, ok: true},
		{name: "it identifies an MCP clientInfo name without a client", raw: "claude-ai", product: "claude-code", client: aivendors.ClientUnknown, ok: true},
		{name: "it prefers an alias over a clientInfo name", raw: "codex-cli", product: "codex", client: aivendors.ClientCLI, ok: true},
		{name: "it identifies a spaced, cased spelling", raw: "  Claude Code ", product: "claude-code", client: aivendors.ClientUnknown, ok: true},
		{name: "it identifies an unspaced spelling", raw: "cursorvscode", product: "cursor", client: aivendors.ClientIDE, ok: true},
		{name: "it identifies an underscored spelling", raw: "claude_code_cli", product: "claude-code", client: aivendors.ClientCLI, ok: true},
		{name: "it keeps chatgpt apart from codex", raw: "chatgpt", product: "chatgpt", client: aivendors.ClientUnknown, ok: true},
		{name: "it identifies gemini as gemini cli", raw: "gemini", product: "gemini-cli", client: aivendors.ClientUnknown, ok: true},
		{name: "it identifies bare claude as nothing", raw: "claude", product: "", client: aivendors.ClientUnknown, ok: false},
		{name: "it identifies an unknown name as nothing", raw: "some-new-agent", product: "", client: aivendors.ClientUnknown, ok: false},
		{name: "it identifies an empty name as nothing", raw: " ", product: "", client: aivendors.ClientUnknown, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			identity, ok := aivendors.Identify(tt.raw)
			require.Equal(t, tt.product, identity.ProductID, "product")
			require.Equal(t, tt.client, identity.Client, "client")
			require.Equal(t, tt.ok, ok, "identified")
		})
	}
}

func TestEveryReportedNameIdentifiesItsOwnProduct(t *testing.T) {
	t.Parallel()

	for _, product := range aivendors.Products() {
		for _, alias := range product.Aliases {
			identity, ok := aivendors.Identify(alias.Name)
			require.True(t, ok, "alias %q of %q", alias.Name, product.ID)
			require.Equal(t, product.ID, identity.ProductID, "alias %q", alias.Name)
			require.Equal(t, alias.Client, identity.Client, "alias %q", alias.Name)
		}
		for _, name := range product.ClientInfoNames {
			identity, ok := aivendors.Identify(name)
			require.True(t, ok, "clientInfo name %q of %q", name, product.ID)
			require.Equal(t, product.ID, identity.ProductID, "clientInfo name %q", name)
		}
	}
}

// spellings are the keys a name is matched under: case-folded with separators
// unified, and the same with the separators removed.
func spellings(name string) []string {
	lowered := strings.ToLower(strings.TrimSpace(name))
	normalized := strings.Join(strings.FieldsFunc(lowered, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '_' || r == '-'
	}), "-")
	return []string{normalized, strings.ReplaceAll(normalized, "-", "")}
}

func TestNoReportedNameBelongsToTwoProducts(t *testing.T) {
	t.Parallel()

	owners := map[string]string{}
	claim := func(name, productID string) {
		for _, spelling := range spellings(name) {
			if owner, taken := owners[spelling]; taken {
				require.Equal(t, owner, productID, "%q (spelled %q) is reported for two products", name, spelling)
			}
			owners[spelling] = productID
		}
	}
	for _, product := range aivendors.Products() {
		for _, alias := range product.Aliases {
			claim(alias.Name, product.ID)
		}
		for _, name := range product.ClientInfoNames {
			claim(name, product.ID)
		}
	}
}
