package authclientbackfill

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// TestRefusesASourceMatchesTheProxy pins the backfill to the proxy's own
// refusal of forwarded sources.
func TestRefusesASourceMatchesTheProxy(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"Authorization", "authorization", "Cookie", "Set-Cookie", "Proxy-Authorization",
		"Gram-Key", "gram_session", "Speakeasy-AI-Session", "X-Gram-Tunnel-Id", "X-Gram-Agent-Version",
		"X-Upstream-Token", "X-Api-Key", "X-Tenant",
	} {
		err := proxy.CheckRemoteHeader(proxy.ConfiguredHeader{IsRequired: false, Name: "X-Upstream", StaticValue: "", ValueFromRequestHeader: src})
		require.Equal(t, errors.Is(err, proxy.ErrProtectedSource), refusesASource([]string{src}), src)
	}
}
