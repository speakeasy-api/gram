package polar

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalog_IsTopUpProductID(t *testing.T) {
	t.Parallel()

	c := &Catalog{ProductIDsTopUp: []string{"prod_a", "prod_b"}}

	require.True(t, c.IsTopUpProductID("prod_a"))
	require.True(t, c.IsTopUpProductID("prod_b"))
	require.False(t, c.IsTopUpProductID("prod_unknown"))
	require.False(t, c.IsTopUpProductID(""))
}
