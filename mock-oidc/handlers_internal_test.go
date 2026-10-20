package mockoidc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExpiresInNeverAdvertisesAnExpiredToken(t *testing.T) {
	t.Parallel()
	require.Equal(t, 1, expiresIn(time.Now().Add(300*time.Millisecond)), "a sub-second lifetime rounds up to one second")
	require.Equal(t, 1, expiresIn(time.Now().Add(-time.Second)), "a lifetime already spent still reports one second")
	require.Equal(t, 120, expiresIn(time.Now().Add(119500*time.Millisecond)))
}
