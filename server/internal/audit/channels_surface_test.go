package audit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// A Channels write is attributed to Channels, not to an unknown surface.
// Anything not on the allowlist records "unknown", which would make every
// Channels-originated change indistinguishable from a classification miss.
func TestChannelsIsAKnownAuditSurface(t *testing.T) {
	t.Parallel()
	require.Equal(t, SurfaceChannels, Surface(contextvalues.ActingSurfaceChannels))
	require.Contains(t, knownSurfaces, SurfaceChannels)
	require.Equal(t, SurfaceChannels, SurfaceFromContext(contextvalues.SetActingSurface(t.Context(), contextvalues.ActingSurfaceChannels)))
	require.False(t, IsStaffAdminSurface(SurfaceChannels))
}
