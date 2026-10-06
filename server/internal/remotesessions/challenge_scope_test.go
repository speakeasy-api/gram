package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
)

func TestClientRequestedScopes(t *testing.T) {
	t.Parallel()

	// The issuer advertises every standard scope plus its own catalogue.
	catalogue := []string{"openid", "profile", "email", "offline_access", "admin", "read:tools"}
	discovered := func(r ResourceScopes) ResourceScopes {
		r.UseDiscovered = true
		return r
	}

	cases := []struct {
		name             string
		client           Client
		resource         ResourceScopes
		wantScopes       []string
		wantWidened      []string
		wantSource       remotesessionmetrics.ScopeSource
		wantUnadvertised []string
	}{
		{
			name:        "client scope beats the resource's challenge scopes, pin, and list",
			client:      Client{ClientScope: []string{"read:tools"}, IssuerScopeOverride: []string{"custom"}, IssuerScopesSupported: catalogue},
			resource:    discovered(ResourceScopes{Pin: []string{"files:read"}, ChallengeScopes: []string{"c"}, ScopesSupported: []string{"files:read", "files:write"}, Live: true}),
			wantScopes:  []string{"read:tools", "openid", "email", "profile", "offline_access"},
			wantWidened: []string{"openid", "email", "profile", "offline_access"},
			wantSource:  remotesessionmetrics.ScopeSourceClientScope,
		},
		{
			name:        "challenge scopes beat the pin",
			client:      Client{IssuerScopesSupported: catalogue},
			resource:    discovered(ResourceScopes{Pin: []string{"files:read"}, ChallengeScopes: []string{"files:write"}, ScopesSupported: []string{"files:read", "files:write"}}),
			wantScopes:  []string{"files:write", "openid", "email", "profile", "offline_access"},
			wantWidened: []string{"openid", "email", "profile", "offline_access"},
			wantSource:  remotesessionmetrics.ScopeSourceChallengeScope,
		},
		{
			name:        "the pin beats the advertised list and the issuer override, and gains the standard scopes",
			client:      Client{IssuerScopeOverride: []string{"custom"}, IssuerScopesSupported: catalogue},
			resource:    discovered(ResourceScopes{Pin: []string{"files:read"}, ScopesSupported: []string{"files:read", "files:write"}, Live: true}),
			wantScopes:  []string{"files:read", "openid", "email", "profile", "offline_access"},
			wantWidened: []string{"openid", "email", "profile", "offline_access"},
			wantSource:  remotesessionmetrics.ScopeSourceResourcePin,
		},
		{
			name:       "the pin is a resource step and needs discovery",
			client:     Client{IssuerScopesSupported: []string{"read:tools"}},
			resource:   ResourceScopes{Pin: []string{"files:read"}},
			wantScopes: []string{"read:tools"},
			wantSource: remotesessionmetrics.ScopeSourceIssuerCatalogue,
		},
		{
			name:             "pinned scopes the resource does not advertise are reported and still sent",
			client:           Client{IssuerScopesSupported: []string{}},
			resource:         discovered(ResourceScopes{Pin: []string{"files:read", "legacy:all"}, ScopesSupported: []string{"files:read"}}),
			wantScopes:       []string{"files:read", "legacy:all"},
			wantSource:       remotesessionmetrics.ScopeSourceResourcePin,
			wantUnadvertised: []string{"legacy:all"},
		},
		{
			name:       "a pin against a resource with no list reports nothing unadvertised",
			client:     Client{},
			resource:   discovered(ResourceScopes{Pin: []string{"legacy:all"}, ScopesSupported: nil}),
			wantScopes: []string{"legacy:all"},
			wantSource: remotesessionmetrics.ScopeSourceResourcePin,
		},
		{
			name:             "a pin against a resource advertising none reports every pinned scope",
			client:           Client{},
			resource:         discovered(ResourceScopes{Pin: []string{"legacy:all"}, ScopesSupported: []string{}}),
			wantScopes:       []string{"legacy:all"},
			wantSource:       remotesessionmetrics.ScopeSourceResourcePin,
			wantUnadvertised: []string{"legacy:all"},
		},
		{
			name:        "client scope is the base and the issuer's standard scopes are appended",
			client:      Client{ClientScope: []string{"read:tools"}, IssuerScopesSupported: catalogue},
			resource:    discovered(ResourceScopes{ChallengeScopes: []string{"c"}, ScopesSupported: []string{"r"}}),
			wantScopes:  []string{"read:tools", "openid", "email", "profile", "offline_access"},
			wantWidened: []string{"openid", "email", "profile", "offline_access"},
			wantSource:  remotesessionmetrics.ScopeSourceClientScope,
		},
		{
			name:       "standard scopes the issuer does not advertise are never added",
			client:     Client{ClientScope: []string{"read:tools"}, IssuerScopesSupported: []string{"read:tools", "write:tools"}},
			wantScopes: []string{"read:tools"},
			wantSource: remotesessionmetrics.ScopeSourceClientScope,
		},
		{
			name:        "challenge scopes beat the resource's advertised list",
			client:      Client{IssuerScopesSupported: catalogue},
			resource:    discovered(ResourceScopes{ChallengeScopes: []string{"files:read"}, ScopesSupported: []string{"files:read", "files:write"}, Live: true}),
			wantScopes:  []string{"files:read", "openid", "email", "profile", "offline_access"},
			wantWidened: []string{"openid", "email", "profile", "offline_access"},
			wantSource:  remotesessionmetrics.ScopeSourceChallengeScope,
		},
		{
			name:        "the live advertised list replaces the issuer catalogue",
			client:      Client{IssuerScopesSupported: catalogue},
			resource:    discovered(ResourceScopes{ScopesSupported: []string{"files:read", "files:write"}, Live: true}),
			wantScopes:  []string{"files:read", "files:write", "openid", "email", "profile", "offline_access"},
			wantWidened: []string{"openid", "email", "profile", "offline_access"},
			wantSource:  remotesessionmetrics.ScopeSourceLiveResource,
		},
		{
			name:       "the cached advertised list is told apart from a live one",
			client:     Client{IssuerScopesSupported: []string{"admin"}},
			resource:   discovered(ResourceScopes{ScopesSupported: []string{"files:read"}}),
			wantScopes: []string{"files:read"},
			wantSource: remotesessionmetrics.ScopeSourceCachedResource,
		},
		{
			name:       "a resource advertising no scopes falls through to the issuer",
			client:     Client{IssuerScopesSupported: []string{"admin", "openid"}},
			resource:   discovered(ResourceScopes{ScopesSupported: []string{}, Live: true}),
			wantScopes: []string{"admin", "openid"},
			wantSource: remotesessionmetrics.ScopeSourceIssuerCatalogue,
		},
		{
			name:       "without discovery the resource row is ignored",
			client:     Client{IssuerScopesSupported: []string{"admin", "openid"}},
			resource:   ResourceScopes{Pin: []string{"p"}, ChallengeScopes: []string{"c"}, ScopesSupported: []string{"files:read"}, Live: true},
			wantScopes: []string{"admin", "openid"},
			wantSource: remotesessionmetrics.ScopeSourceIssuerCatalogue,
		},
		{
			name:       "the issuer's scope override is requested verbatim",
			client:     Client{IssuerScopesSupported: catalogue, IssuerScopeOverride: []string{"custom:one", "custom:two"}},
			resource:   discovered(ResourceScopes{ScopesSupported: []string{}}),
			wantScopes: []string{"custom:one", "custom:two"},
			wantSource: remotesessionmetrics.ScopeSourceIssuerOverride,
		},
		{
			name:       "the issuer's scope override yields to the client scope and the resource",
			client:     Client{IssuerScopesSupported: []string{"admin"}, IssuerScopeOverride: []string{"custom"}},
			resource:   discovered(ResourceScopes{ScopesSupported: []string{"files:read"}}),
			wantScopes: []string{"files:read"},
			wantSource: remotesessionmetrics.ScopeSourceCachedResource,
		},
		{
			name:        "an empty override is unset and falls through",
			client:      Client{ClientScope: []string{"read:tools"}, IssuerScopesSupported: []string{"openid"}, IssuerScopeOverride: []string{}},
			wantScopes:  []string{"read:tools", "openid"},
			wantWidened: []string{"openid"},
			wantSource:  remotesessionmetrics.ScopeSourceClientScope,
		},
		{
			name:       "issuer scopes_supported is the base when nothing else names one, and appending is a no-op",
			client:     Client{IssuerScopesSupported: []string{"openid", "profile"}},
			wantScopes: []string{"openid", "profile"},
			wantSource: remotesessionmetrics.ScopeSourceIssuerCatalogue,
		},
		{
			name:       "an empty client scope is the same as none",
			client:     Client{ClientScope: []string{}, IssuerScopesSupported: []string{"openid"}},
			wantScopes: []string{"openid"},
			wantSource: remotesessionmetrics.ScopeSourceIssuerCatalogue,
		},
		{
			name:       "a NULL issuer scopes_supported leaves the client scope alone",
			client:     Client{ClientScope: []string{"read:tools"}, IssuerScopesSupported: nil},
			wantScopes: []string{"read:tools"},
			wantSource: remotesessionmetrics.ScopeSourceClientScope,
		},
		{
			name:        "a standard scope already in the base is not repeated or counted as widening",
			client:      Client{ClientScope: []string{"openid", "read:tools"}, IssuerScopesSupported: []string{"openid", "email"}},
			wantScopes:  []string{"openid", "read:tools", "email"},
			wantWidened: []string{"email"},
			wantSource:  remotesessionmetrics.ScopeSourceClientScope,
		},
		{
			name:       "nothing configured requests nothing",
			client:     Client{},
			wantSource: remotesessionmetrics.ScopeSourceNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.client.RequestedScopes(tc.resource)
			if tc.wantScopes == nil {
				require.Empty(t, got.Scopes)
			} else {
				require.Equal(t, tc.wantScopes, got.Scopes)
			}
			require.Equal(t, tc.wantWidened, got.Widened)
			require.Equal(t, tc.wantSource, got.Source)
			require.Equal(t, tc.wantUnadvertised, got.Unadvertised)
		})
	}
}

// The resolved set is a copy: mutating it must not reach back into the
// client's stored scope, the pin, or the override.
func TestClientRequestedScopes_DoesNotAliasInputs(t *testing.T) {
	t.Parallel()

	override := Client{IssuerScopeOverride: []string{"a"}}
	scopes := override.RequestedScopes(ResourceScopes{}).Scopes
	scopes[0] = "mutated"
	require.Equal(t, []string{"a"}, override.IssuerScopeOverride)

	base := Client{ClientScope: []string{"a"}, IssuerScopesSupported: []string{"openid"}}
	scopes = base.RequestedScopes(ResourceScopes{}).Scopes
	scopes[0] = "mutated"
	require.Equal(t, []string{"a"}, base.ClientScope)

	pin := []string{"a"}
	scopes = Client{}.RequestedScopes(ResourceScopes{Pin: pin, UseDiscovered: true}).Scopes
	scopes[0] = "mutated"
	require.Equal(t, []string{"a"}, pin)
}
