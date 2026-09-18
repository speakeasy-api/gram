package remotesessions

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestFederatedOfflinePolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                        string
		advertised                  []string
		optedIn, requested, enabled bool
	}{
		{"advertised", []string{"openid", "offline_access"}, true, true, true},
		{"omitted administrator override", nil, true, true, true},
		{"explicit exclusion", []string{"openid", "email"}, true, true, false},
		{"present empty excludes", []string{}, true, true, false},
		{"not opted in", []string{"offline_access"}, false, true, false},
		{"minimal login", []string{"offline_access"}, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := federatedFixture(t)
			p.metadata.ScopesSupported = tc.advertised
			if !tc.optedIn {
				p.client.Scope = []string{"openid", "email", "profile"}
			}
			// An issuer override cannot add service scopes or bypass client opt-in.
			p.issuer.ScopeOverride = []string{"openid", "email", "downstream:admin", "offline_access"}
			policy, err := p.OfflinePolicy()
			require.NoError(t, err)
			require.Equal(t, tc.enabled, policy.Enabled)
			target, err := p.BuildAuthorizationURLWithOffline("https://gram.example.test/callback", "state", "nonce", strings.Repeat("a", 43), tc.requested)
			require.NoError(t, err)
			require.Contains(t, target.Query().Get("scope"), "profile")
			require.NotContains(t, target.Query().Get("scope"), "downstream:admin")
			require.Equal(t, tc.enabled && tc.requested, strings.Contains(target.Query().Get("scope"), "offline_access"))
			if tc.enabled && tc.requested {
				require.Equal(t, "consent", target.Query().Get("prompt"))
			} else {
				require.Empty(t, target.Query().Get("prompt"))
			}
		})
	}
	for _, scopes := range [][]string{{"openid"}, {"email"}, {"offline_access"}} {
		p := federatedFixture(t)
		p.client.Scope = scopes
		_, err := p.OfflinePolicy()
		require.ErrorIs(t, err, ErrFederatedConfiguration)
	}
}

func TestFederatedOfflineConfigurationHash(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	original := p.OfflineConfigurationHash()
	require.NotEmpty(t, original)
	p.client.Scope = []string{"profile", "offline_access", "email", "openid", "email"}
	p.issuer.MetadataFetchedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	p.issuer.UpdatedAt = p.issuer.MetadataFetchedAt
	p.metadata.ScopesSupported = nil
	require.Equal(t, original, p.OfflineConfigurationHash())
	p.client.UpdatedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	require.Equal(t, original, p.OfflineConfigurationHash(), "unrelated client edits must not reset refusal")
	revision := p.OfflineConfigurationHash()
	p.metadata.ScopesSupported = []string{"offline_access", "openid"}
	require.NotEqual(t, revision, p.OfflineConfigurationHash())
	advertised := p.OfflineConfigurationHash()
	p.metadata.ScopesSupported = []string{"openid", "email", "offline_access", "profile"}
	require.Equal(t, advertised, p.OfflineConfigurationHash(), "unrelated provider capabilities do not reset suppression")
	p.client.Scope = []string{"openid", "email"}
	require.NotEqual(t, advertised, p.OfflineConfigurationHash())
	beforeSecretRotation := p.OfflineConfigurationHash()
	p.client.ClientSecretEncrypted.String = "rotated-ciphertext"
	require.NotEqual(t, beforeSecretRotation, p.OfflineConfigurationHash(), "credential replacement must change the revision even within one transaction")
}

func TestFederatedDelegationConfigurationHash(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	original := p.DelegationConfigurationHash()
	require.NotEmpty(t, original)
	require.Equal(t, original, FederatedDelegationConfigurationHash(p.organizationID, p.issuer, p.client))
	p.metadata.ScopesSupported = []string{"offline_access"}
	p.metadata.AuthorizationEndpoint += "/discovered"
	require.Equal(t, original, p.DelegationConfigurationHash(), "live metadata is not the stored registration revision")
	p.client.UpdatedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	p.issuer.UpdatedAt = p.client.UpdatedAt
	p.issuer.MetadataFetchedAt = p.client.UpdatedAt
	p.issuer.Jwks = []byte(`{"keys":[]}`)
	p.client.ResourceName = pgtype.Text{String: "renamed", Valid: true}
	p.client.Scope = []string{"profile", "email", "openid", "offline_access", "openid"}
	require.Equal(t, original, p.DelegationConfigurationHash())
	for _, change := range []struct {
		name   string
		mutate func(*FederatedProvider)
	}{
		{"credential", func(p *FederatedProvider) { p.client.ClientSecretEncrypted.String = "rotated" }},
		{"client", func(p *FederatedProvider) { p.client.ClientID = "another-registration" }},
		{"issuer", func(p *FederatedProvider) { p.issuer.Issuer += "/other" }},
		{"jwks", func(p *FederatedProvider) {
			p.issuer.JwksUri = pgtype.Text{String: "https://idp.example.test/new-keys", Valid: true}
		}},
		{"auth method", func(p *FederatedProvider) { p.client.TokenEndpointAuthMethod.String = "private_key_jwt" }},
		{"scope", func(p *FederatedProvider) { p.client.Scope = append(p.client.Scope, "extra") }},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Parallel()
			changed := *p
			changed.client.Scope = append([]string(nil), p.client.Scope...)
			change.mutate(&changed)
			require.NotEqual(t, original, changed.DelegationConfigurationHash())
		})
	}
}

func TestFederatedClientAllowlistIgnoresIssuerScopeOverride(t *testing.T) {
	t.Parallel()
	fixture := federatedFixture(t)
	fixture.issuer.ScopeOverride = []string{"downstream:admin"}
	provider, err := newFederatedProvider(fixture.organizationID, fixture.issuer, fixture.client, fixture.metadata)
	require.NoError(t, err, "issuer override cannot reject valid upstream client scopes")
	target, err := provider.BuildAuthorizationURL("https://gram.example.test/callback", "state", "nonce", strings.Repeat("a", 43))
	require.NoError(t, err)
	require.Equal(t, "openid email profile", target.Query().Get("scope"))
	fixture.client.Scope = []string{"openid"}
	fixture.issuer.ScopeOverride = []string{"openid", "email"}
	_, err = newFederatedProvider(fixture.organizationID, fixture.issuer, fixture.client, fixture.metadata)
	require.ErrorIs(t, err, ErrFederatedConfiguration, "issuer override cannot repair an invalid client allowlist")
}

func TestFederatedOfflinePolicyDiscoveryPresence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, document string
		enabled        bool
	}{
		{"omitted", `{}`, true},
		{"null", `{"scopes_supported":null}`, false},
		{"empty", `{"scopes_supported":[]}`, false},
		{"excluded", `{"scopes_supported":["openid","email"]}`, false},
		{"advertised", `{"scopes_supported":["offline_access"]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := federatedFixture(t)
			p.client.Scope = []string{"openid", "email", "offline_access"}
			p.metadata.ScopesSupported = nil
			require.NoError(t, json.Unmarshal([]byte(tc.document), &p.metadata))
			policy, err := p.OfflinePolicy()
			require.NoError(t, err)
			require.Equal(t, tc.enabled, policy.Enabled)
			cached, err := json.Marshal(p.metadata)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(cached, &p.metadata))
			fromCache, err := p.OfflinePolicy()
			require.NoError(t, err)
			require.Equal(t, policy, fromCache)
		})
	}
}
