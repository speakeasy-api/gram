package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientRequestedScopes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		client        Client
		wantScopes    []string
		wantWidened   []string
		wantNilScopes bool
	}{
		{
			name: "client scope is the base and the issuer's standard scopes are appended",
			client: Client{
				ClientScope:           []string{"read:tools"},
				IssuerScopesSupported: []string{"openid", "profile", "email", "offline_access", "admin"},
			},
			wantScopes:  []string{"read:tools", "openid", "email", "profile", "offline_access"},
			wantWidened: []string{"openid", "email", "profile", "offline_access"},
		},
		{
			name: "standard scopes the issuer does not advertise are never added",
			client: Client{
				ClientScope:           []string{"read:tools"},
				IssuerScopesSupported: []string{"read:tools", "write:tools"},
			},
			wantScopes:  []string{"read:tools"},
			wantWidened: nil,
		},
		{
			name: "issuer scopes_supported is the base when the client has no scope",
			client: Client{
				ClientScope:           nil,
				IssuerScopesSupported: []string{"openid", "profile"},
			},
			wantScopes:  []string{"openid", "profile"},
			wantWidened: nil,
		},
		{
			name: "an empty client scope is the same as none",
			client: Client{
				ClientScope:           []string{},
				IssuerScopesSupported: []string{"openid"},
			},
			wantScopes:  []string{"openid"},
			wantWidened: nil,
		},
		{
			name: "a standard scope already in the client scope is not repeated or counted as widening",
			client: Client{
				ClientScope:           []string{"openid", "read:tools"},
				IssuerScopesSupported: []string{"openid", "email"},
			},
			wantScopes:  []string{"openid", "read:tools", "email"},
			wantWidened: []string{"email"},
		},
		{
			name: "the issuer's scope override is requested verbatim",
			client: Client{
				ClientScope:           []string{"read:tools"},
				IssuerScopesSupported: []string{"openid", "email", "offline_access"},
				IssuerScopeOverride:   []string{"custom:one", "custom:two"},
			},
			wantScopes:  []string{"custom:one", "custom:two"},
			wantWidened: nil,
		},
		{
			name: "an empty override requests no scope at all",
			client: Client{
				ClientScope:           []string{"read:tools"},
				IssuerScopesSupported: []string{"openid"},
				IssuerScopeOverride:   []string{},
			},
			wantScopes:  []string{},
			wantWidened: nil,
		},
		{
			name:          "nothing configured requests nothing",
			client:        Client{},
			wantNilScopes: true,
			wantWidened:   nil,
		},
	}

	for _, tc := range cases {
		scopes, widened := tc.client.RequestedScopes()
		if tc.wantNilScopes {
			require.Empty(t, scopes, tc.name)
		} else {
			require.Equal(t, tc.wantScopes, scopes, tc.name)
		}
		require.Equal(t, tc.wantWidened, widened, tc.name)
	}
}

// The resolved set is a copy: mutating it must not reach back into the
// client's stored scope or override.
func TestClientRequestedScopes_DoesNotAliasInputs(t *testing.T) {
	t.Parallel()

	override := Client{IssuerScopeOverride: []string{"a"}}
	scopes, _ := override.RequestedScopes()
	scopes[0] = "mutated"
	require.Equal(t, []string{"a"}, override.IssuerScopeOverride)

	base := Client{ClientScope: []string{"a"}, IssuerScopesSupported: []string{"openid"}}
	scopes, _ = base.RequestedScopes()
	scopes[0] = "mutated"
	require.Equal(t, []string{"a"}, base.ClientScope)
}
