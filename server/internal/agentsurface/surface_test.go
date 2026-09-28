package agentsurface_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agentsurface"
)

func TestForHookSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		variant string
		want    agentsurface.Surface
		known   bool
	}{
		{name: "it folds the canonical claude code source", source: "claude-code", variant: "", want: agentsurface.SurfaceClaudeCode, known: true},
		{name: "it folds underscore spelling", source: "claude_code", variant: "", want: agentsurface.SurfaceClaudeCode, known: true},
		{name: "it folds unspaced spelling", source: "claudecode", variant: "", want: agentsurface.SurfaceClaudeCode, known: true},
		{name: "it folds mixed case and padding", source: "  Claude-Desktop  ", variant: "", want: agentsurface.SurfaceClaudeChat, known: true},
		{name: "it folds chatgpt onto codex", source: "chatgpt", variant: "", want: agentsurface.SurfaceCodex, known: true},
		{name: "it folds third-party agents onto other", source: "opencode", variant: "", want: agentsurface.SurfaceOther, known: true},
		{name: "it folds claude code desktop", source: "claude-code-desktop", variant: "", want: agentsurface.SurfaceClaudeCode, known: true},
		{name: "it folds claude tag onto claude code", source: "claude-tag", variant: "", want: agentsurface.SurfaceClaudeCode, known: true},
		{name: "it folds claude chat web", source: "claude-chat-web", variant: "", want: agentsurface.SurfaceClaudeChat, known: true},

		// Bare "claude" covers three products, so only the variant resolves it.
		{name: "it resolves bare claude through the cowork variant", source: "claude", variant: "cowork", want: agentsurface.SurfaceCowork, known: true},
		{name: "it resolves bare claude through the claude-code variant", source: "claude", variant: "claude-code", want: agentsurface.SurfaceClaudeCode, known: true},
		{name: "it leaves bare claude unattributed without a variant", source: "claude", variant: "", want: agentsurface.SurfaceUnknown, known: true},

		{name: "it reports an unmapped source rather than discarding it", source: "some-new-agent", variant: "", want: agentsurface.SurfaceUnknown, known: false},
		{name: "it treats an empty source as unmapped", source: "", variant: "", want: agentsurface.SurfaceUnknown, known: false},
		{name: "it treats a whitespace-only source as unmapped", source: "   ", variant: "", want: agentsurface.SurfaceUnknown, known: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, known := agentsurface.ForHookSource(tt.source, tt.variant)
			require.Equal(t, tt.want, got, "surface")
			require.Equal(t, tt.known, known, "recognized")
		})
	}
}

func TestAllOmitsUnknown(t *testing.T) {
	t.Parallel()

	// Ordered, not set-compared: the matrix renders one column per entry in
	// this order, so a reordering is user-visible.
	require.Equal(t, []agentsurface.Surface{
		agentsurface.SurfaceClaudeCode,
		agentsurface.SurfaceClaudeChat,
		agentsurface.SurfaceCowork,
		agentsurface.SurfaceCodex,
		agentsurface.SurfaceCursor,
		agentsurface.SurfaceOther,
	}, agentsurface.All)
	require.NotContains(t, agentsurface.All, agentsurface.SurfaceUnknown)
}
