package remotesessions

import (
	"testing"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

func TestIssuerViewChangedIgnoresRefreshTracking(t *testing.T) {
	t.Parallel()
	before := types.RemoteSessionIssuer{
		UpdatedAt:          "2026-01-01T00:00:00Z",
		JwksURI:            conv.PtrEmpty("https://issuer.example/jwks"),
		JwksFetchedAt:      conv.PtrEmpty("2026-01-01T00:00:00Z"),
		JwksCacheExpiresAt: conv.PtrEmpty("2026-01-01T00:05:00Z"),
	}
	after := before
	after.UpdatedAt = "2026-01-02T00:00:00Z"
	after.JwksFetchedAt = conv.PtrEmpty("2026-01-02T00:00:00Z")
	after.JwksCacheExpiresAt = conv.PtrEmpty("2026-01-02T00:05:00Z")
	require.False(t, issuerViewChanged(&before, &after), "cache freshness alone must not create an issuer-update audit")
	require.Equal(t, "2026-01-01T00:00:00Z", *before.JwksFetchedAt, "comparison must not mutate audit snapshots")
	require.Equal(t, "2026-01-02T00:05:00Z", *after.JwksCacheExpiresAt)
	after.JwksURI = conv.PtrEmpty("https://issuer.example/replacement-jwks")
	require.True(t, issuerViewChanged(&before, &after), "key source changes are still audited")
	after.JwksURI = before.JwksURI
	after.ScopesSupported = []string{"new-scope"}
	require.True(t, issuerViewChanged(&before, &after), "capability changes are still audited")
}
