package shadowmcp

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestEffectiveDisposition(t *testing.T) {
	t.Parallel()

	require.Empty(t, EffectiveDisposition(pgtype.Text{}, []string{SourceShadowMCP}, "flag"))
	require.Empty(t, EffectiveDisposition(pgtype.Text{}, []string{"gitleaks"}, "block"))
	require.Equal(t, DispositionBlockAll, EffectiveDisposition(pgtype.Text{}, []string{SourceShadowMCP}, "block"))
	require.Equal(t, DispositionAllowAll, EffectiveDisposition(
		pgtype.Text{String: DispositionAllowAll, Valid: true},
		[]string{SourceShadowMCP},
		"block",
	))
}

func TestBlockedURLMatch_CanonicalizesBeforeMatching(t *testing.T) {
	t.Parallel()

	blocked := []string{"https://sketchy.example.com/mcp"}

	matched, ok := BlockedURLMatch(blocked, "HTTPS://SKETCHY.EXAMPLE.COM:443/mcp?token=ignored")
	require.True(t, ok)
	require.Equal(t, "https://sketchy.example.com/mcp", matched)
}

func TestBlockedURLMatch_NoMatch(t *testing.T) {
	t.Parallel()

	blocked := []string{"https://sketchy.example.com/mcp"}

	matched, ok := BlockedURLMatch(blocked, "https://fine.example.com/mcp")
	require.False(t, ok)
	require.Empty(t, matched)
}

func TestBlockedURLMatch_EmptyInputs(t *testing.T) {
	t.Parallel()

	matched, ok := BlockedURLMatch(nil, "https://sketchy.example.com/mcp")
	require.False(t, ok)
	require.Empty(t, matched)

	matched, ok = BlockedURLMatch([]string{"https://sketchy.example.com/mcp"}, "")
	require.False(t, ok)
	require.Empty(t, matched)
}

func TestBlockedURLMatch_UncanonicalizableURLAllowed(t *testing.T) {
	t.Parallel()

	matched, ok := BlockedURLMatch([]string{"https://sketchy.example.com/mcp"}, "not a url")
	require.False(t, ok)
	require.Empty(t, matched)
}
