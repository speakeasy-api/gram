package tunneledmcp

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestEffectiveTunneledMcpServerLimit(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		accountType string
		want        int64
	}{
		{accountType: "free", want: 0},
		{accountType: "pro", want: 10},
		{accountType: "payg", want: 25},
		{accountType: "enterprise", want: 25},
		{accountType: "", want: 0},
	} {
		t.Run(tt.accountType, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, effectiveTunneledMcpServerLimit(tt.accountType, pgtype.Int4{}))
		})
	}

	require.EqualValues(t, 3, effectiveTunneledMcpServerLimit("enterprise", pgtype.Int4{Int32: 3, Valid: true}))
	require.EqualValues(t, 0, effectiveTunneledMcpServerLimit("enterprise", pgtype.Int4{Int32: 0, Valid: true}))
}
