package remotesessions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// FederatedOfflinePolicy describes only the upstream registration's allowlist.
// Its hash deliberately excludes discovery/cache timestamps and raw secrets.
type FederatedOfflinePolicy struct {
	ConfigurationHash string
	Enabled           bool
	Scopes            []string
}

func (p *FederatedProvider) OfflinePolicy() (FederatedOfflinePolicy, error) {
	if p == nil {
		return FederatedOfflinePolicy{}, ErrFederatedConfiguration
	}
	scopes := normalizedFederatedScopes(p.client.Scope)
	if !slices.Contains(scopes, "openid") || !slices.Contains(scopes, "email") {
		return FederatedOfflinePolicy{}, ErrFederatedConfiguration
	}
	advertised := p.metadata.ScopesSupported
	enabled := slices.Contains(scopes, "offline_access") && (advertised == nil || slices.Contains(advertised, "offline_access"))
	// The encrypted credential digest identifies rotation without retaining secrets.
	// Do not use the provider fingerprint: discovery order and unrelated metadata
	// must not reset a human's refusal suppression.
	encoded, err := json.Marshal(federatedOfflinePolicyRevision{
		Registration:  p.DelegationConfigurationHash(),
		Authorization: p.metadata.AuthorizationEndpoint, Token: p.metadata.TokenEndpoint, JWKS: p.metadata.JwksURI,
		AuthMethods:       normalizedFederatedScopes(p.metadata.TokenEndpointAuthMethodsSupported),
		OfflineCapability: offlineCapability(advertised),
	})
	if err != nil {
		return FederatedOfflinePolicy{}, ErrFederatedConfiguration
	}
	digest := sha256.Sum256(encoded)
	return FederatedOfflinePolicy{ConfigurationHash: hex.EncodeToString(digest[:]), Enabled: enabled, Scopes: slices.Clone(p.client.Scope)}, nil
}

func offlineCapability(scopes []string) string {
	if scopes == nil {
		return "unspecified"
	}
	if slices.Contains(scopes, "offline_access") {
		return "advertised"
	}
	return "excluded"
}

func normalizedFederatedScopes(scopes []string) []string {
	result := slices.Clone(scopes)
	slices.Sort(result)
	return slices.Compact(result)
}

// BuildAuthorizationURLWithOffline retains the existing URL hardening while
// selecting scopes exclusively from this upstream client's allowlist.
func (p *FederatedProvider) BuildAuthorizationURLWithOffline(callbackURL, state, nonce, verifier string, offline bool) (*url.URL, error) {
	policy, err := p.OfflinePolicy()
	if err != nil {
		return nil, err
	}
	target, err := p.BuildAuthorizationURL(callbackURL, state, nonce, verifier)
	if err != nil {
		return nil, err
	}
	scopes := make([]string, 0, len(policy.Scopes))
	for _, scope := range policy.Scopes {
		if scope != "offline_access" || (offline && policy.Enabled) {
			scopes = append(scopes, scope)
		}
	}
	q := target.Query()
	q.Set("scope", strings.Join(scopes, " "))
	q.Del("prompt")
	if offline && policy.Enabled {
		q.Set("prompt", "consent")
	}
	target.RawQuery = q.Encode()
	return target, nil
}

// OfflineConfigurationHash is stable across discovery refreshes and identifies
// the policy revision used for per-human refusal suppression.
func (p *FederatedProvider) OfflineConfigurationHash() string {
	policy, err := p.OfflinePolicy()
	if err != nil {
		return ""
	}
	return policy.ConfigurationHash
}

// DelegationConfigurationHash binds retained credentials to the current stored
// registration. Unlike refusal policy, this revision needs no live discovery.
func (p *FederatedProvider) DelegationConfigurationHash() string {
	if p == nil {
		return ""
	}
	return FederatedDelegationConfigurationHash(p.organizationID, p.issuer, p.client, p.signingKeyRevision)
}

// FederatedDelegationConfigurationHash is suitable for network-free management
// reads. Cache timestamps, display metadata, JWKS cache bytes and generic row
// UpdatedAt are deliberately excluded. Callers using private_key_jwt supply
// the active signing-key revision read from the tenant-scoped key metadata.
func FederatedDelegationConfigurationHash(organizationID string, issuer repo.RemoteSessionIssuer, client repo.RemoteSessionClient, signingRevision ...string) string {
	revision := ""
	if len(signingRevision) > 0 {
		revision = signingRevision[0]
	}
	scopes := normalizedFederatedScopes(client.Scope)
	if !slices.Contains(scopes, "openid") || !slices.Contains(scopes, "email") {
		return ""
	}
	secretRevision := sha256.Sum256([]byte(client.ClientSecretEncrypted.String))
	encoded, err := json.Marshal(federatedDelegationRegistrationRevision{
		Organization: organizationID, IssuerID: issuer.ID.String(), Issuer: issuer.Issuer,
		ClientID: client.ID.String(), Client: client.ClientID,
		Method: client.TokenEndpointAuthMethod.String, Audience: client.TokenEndpointAuthAudienceFormat.String,
		Key: client.JsonWebKeySetID.UUID.String(), CredentialRevision: hex.EncodeToString(secretRevision[:]),
		Authorization: issuer.AuthorizationEndpoint.String, Token: issuer.TokenEndpoint.String, JWKS: issuer.JwksUri.String,
		Tunnel: issuer.TunneledMcpServerID.UUID.String(), SecretExpiry: client.ClientSecretExpiresAt.Time.UTC().String(),
		Scopes: scopes, SigningRevision: revision,
	})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// federatedOfflinePolicyRevision is the persisted refusal-suppression hash input.
// Field names, order and JSON encoding are part of the revision format: changing
// them invalidates existing suppression even if the provider policy is unchanged.
type federatedOfflinePolicyRevision struct {
	Registration      string   `json:"Registration"`
	Authorization     string   `json:"Authorization"`
	Token             string   `json:"Token"`
	JWKS              string   `json:"JWKS"`
	OfflineCapability string   `json:"OfflineCapability"`
	AuthMethods       []string `json:"AuthMethods"`
}

// federatedDelegationRegistrationRevision binds retained credentials to the
// registration. Preserve field names, order and encoding to avoid invalidating
// stored credentials on a cosmetic refactor. This is internal, not an OIDC format.
type federatedDelegationRegistrationRevision struct {
	Organization       string   `json:"Organization"`
	IssuerID           string   `json:"IssuerID"`
	Issuer             string   `json:"Issuer"`
	ClientID           string   `json:"ClientID"`
	Client             string   `json:"Client"`
	Method             string   `json:"Method"`
	Audience           string   `json:"Audience"`
	Key                string   `json:"Key"`
	CredentialRevision string   `json:"CredentialRevision"`
	Authorization      string   `json:"Authorization"`
	Token              string   `json:"Token"`
	JWKS               string   `json:"JWKS"`
	Tunnel             string   `json:"Tunnel"`
	SecretExpiry       string   `json:"SecretExpiry"`
	SigningRevision    string   `json:"SigningRevision"`
	Scopes             []string `json:"Scopes"`
}
