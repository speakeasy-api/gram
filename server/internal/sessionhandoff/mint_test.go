package sessionhandoff

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClampHandoffTTL(t *testing.T) {
	t.Parallel()
	below, above, exact := 1, 7200, 300
	require.Equal(t, 15*time.Minute, ClampHandoffTTL(nil))
	require.Equal(t, time.Minute, ClampHandoffTTL(&below))
	require.Equal(t, time.Hour, ClampHandoffTTL(&above))
	require.Equal(t, 5*time.Minute, ClampHandoffTTL(&exact))
}
