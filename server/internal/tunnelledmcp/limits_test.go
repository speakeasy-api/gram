package tunnelledmcp

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestEffectiveTunnelledMcpServerLimit(t *testing.T) {
	t.Parallel()

	require.EqualValues(t, 0, effectiveTunnelledMcpServerLimit("free", pgtype.Int4{}))
	require.EqualValues(t, 10, effectiveTunnelledMcpServerLimit("pro", pgtype.Int4{}))
	require.EqualValues(t, 25, effectiveTunnelledMcpServerLimit("enterprise", pgtype.Int4{}))
	require.EqualValues(t, 0, effectiveTunnelledMcpServerLimit("", pgtype.Int4{}))
	require.EqualValues(t, 3, effectiveTunnelledMcpServerLimit("enterprise", pgtype.Int4{Int32: 3, Valid: true}))
	require.EqualValues(t, 0, effectiveTunnelledMcpServerLimit("enterprise", pgtype.Int4{Int32: 0, Valid: true}))
}
