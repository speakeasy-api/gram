package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientResolveScopes(t *testing.T) {
	t.Parallel()

	t.Run("prefers client override when set", func(t *testing.T) {
		t.Parallel()
		c := Client{
			ClientScope:           []string{"read:tools"},
			IssuerScopesSupported: []string{"openid", "profile"},
		}
		require.Equal(t, []string{"read:tools"}, c.resolveScopes())
	})

	t.Run("falls back to issuer scopes_supported when client override is nil", func(t *testing.T) {
		t.Parallel()
		c := Client{
			ClientScope:           nil,
			IssuerScopesSupported: []string{"openid", "profile"},
		}
		require.Equal(t, []string{"openid", "profile"}, c.resolveScopes())
	})

	t.Run("falls back to issuer scopes_supported when client override is empty", func(t *testing.T) {
		t.Parallel()
		c := Client{
			ClientScope:           []string{},
			IssuerScopesSupported: []string{"openid"},
		}
		require.Equal(t, []string{"openid"}, c.resolveScopes())
	})
}
