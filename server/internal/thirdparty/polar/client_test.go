package polar

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCostToCredits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cost float64
		want float64
	}{
		{"zero", 0, 0},
		{"sub-credit rounds up", 0.0001, 1},
		{"exactly one credit", 0.001, 1},
		{"twenty dollars", 20, 20000},
		{"fractional rounds up", 0.0042, 5},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := costToCredits(c.cost)
			if c.want == 0 {
				require.Zero(t, got)
				return
			}
			require.InDelta(t, c.want, got, 0.0001)
		})
	}
}

func TestCatalog_IsTopUpProductID(t *testing.T) {
	t.Parallel()

	c := &Catalog{ProductIDsTopUp: []string{"prod_a", "prod_b"}}

	require.True(t, c.IsTopUpProductID("prod_a"))
	require.True(t, c.IsTopUpProductID("prod_b"))
	require.False(t, c.IsTopUpProductID("prod_unknown"))
	require.False(t, c.IsTopUpProductID(""))
}
