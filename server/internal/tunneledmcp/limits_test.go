package tunneledmcp

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestEffectiveTunneledMcpServerLimit(t *testing.T) {
	t.Parallel()

	require.EqualValues(t, 0, effectiveTunneledMcpServerLimit("free", pgtype.Int4{}))
	require.EqualValues(t, 10, effectiveTunneledMcpServerLimit("pro", pgtype.Int4{}))
	require.EqualValues(t, 25, effectiveTunneledMcpServerLimit("enterprise", pgtype.Int4{}))
	require.EqualValues(t, 0, effectiveTunneledMcpServerLimit("", pgtype.Int4{}))
	require.EqualValues(t, 3, effectiveTunneledMcpServerLimit("enterprise", pgtype.Int4{Int32: 3, Valid: true}))
	require.EqualValues(t, 0, effectiveTunneledMcpServerLimit("enterprise", pgtype.Int4{Int32: 0, Valid: true}))
}
