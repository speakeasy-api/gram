package remotesessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

const federatedMetadataTTL = time.Minute

type federatedMetadataCacheEntry struct {
	Document  rfc8414Document `json:"Document"`
	ExpiresAt time.Time       `json:"ExpiresAt"`
	// The shared issuer document serializes nil scopes as null. Preserve omission
	// only in this OIDC cache, where it controls the offline-access override.
	ScopesOmitted bool `json:"ScopesOmitted"`
}

// updated_at changes on JWKS cache writes as well as administrator edits. Bind
// the effective configuration instead, excluding only cache state/timestamps.
// Ownership, attachment tier, stored metadata/policies and tunnel are included.
func federatedIssuerVersion(issuer repo.RemoteSessionIssuer) string {
	issuer.Jwks = nil
	issuer.JwksFetchedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	issuer.JwksCacheExpiresAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	issuer.JwksLastErrorAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	issuer.JwksLastError = pgtype.Text{String: "", Valid: false}
	issuer.JwksEtag = pgtype.Text{String: "", Valid: false}
	issuer.MetadataFetchedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	issuer.MetadataLastErrorAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	issuer.MetadataLastError = pgtype.Text{String: "", Valid: false}
	issuer.MetadataLastErrorUrl = pgtype.Text{String: "", Valid: false}
	issuer.UpdatedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	encoded, _ := json.Marshal(federatedIssuerSnapshot(issuer))
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func federatedMetadataCacheKey(organizationID string, issuer repo.RemoteSessionIssuer) string {
	encoded, _ := json.Marshal([]string{organizationID, issuer.ID.String(), issuer.Issuer, federatedIssuerVersion(issuer)})
	sum := sha256.Sum256(encoded)
	return "federated-oidc-metadata:v3:" + hex.EncodeToString(sum[:])
}

// validateFederatedHost applies preflight DNS/IP policy as well as the runtime
// Guardian dial checks. A tunnel binding permits private server-side endpoints
// only: browser redirects never traverse that tunnel and remain policy checked.
func (m *ChallengeManager) validateFederatedHost(ctx context.Context, rawURL string, tunneled bool) error {
	u, err := url.Parse(rawURL)
	if err != nil || !validIssuerDiscoveryURL(u) || u.Fragment != "" {
		return ErrFederatedConfiguration
	}
	if tunneled {
		if m.tunnels == nil {
			return ErrFederatedConfiguration
		}
		return nil
	}
	if m.policy == nil || m.policy.ValidateHost(ctx, u.Hostname()) != nil {
		return ErrFederatedConfiguration
	}
	return nil
}

func (m *ChallengeManager) validateFederatedMetadataHosts(ctx context.Context, issuer repo.RemoteSessionIssuer, doc rfc8414Document) error {
	if doc.Issuer != issuer.Issuer {
		return ErrFederatedConfiguration
	}
	issuerURL, err := url.Parse(issuer.Issuer)
	if err != nil || validateIssuerMetadataEndpoints(doc, issuerURL) != nil {
		return ErrFederatedConfiguration
	}
	if err := m.validateFederatedHost(ctx, doc.AuthorizationEndpoint, false); err != nil {
		return err
	}
	for _, endpoint := range []string{issuer.Issuer, doc.TokenEndpoint, doc.JwksURI} {
		if err := m.validateFederatedHost(ctx, endpoint, issuer.TunneledMcpServerID.Valid); err != nil {
			return err
		}
	}
	return nil
}

// This cache contains only the typed discovery document, never a client row,
// encrypted/decrypted secret, or raw discovery extensions. Hits still pass live
// issuer configuration and host validation. Outages fall back to bounded fetch;
// expired cache entries are never used as stale metadata.
func (m *ChallengeManager) loadFederatedMetadata(ctx context.Context, organizationID string, issuer repo.RemoteSessionIssuer, doer httpDoer) (rfc8414Document, error) {
	if err := m.validateFederatedHost(ctx, issuer.Issuer, issuer.TunneledMcpServerID.Valid); err != nil {
		return rfc8414Document{}, err
	}
	key := federatedMetadataCacheKey(organizationID, issuer)
	if m.locks != nil {
		var entry federatedMetadataCacheEntry
		if m.locks.Get(ctx, key, &entry) == nil && entry.ExpiresAt.After(time.Now()) {
			entry.restoreScopePresence()
			if err := m.validateFederatedMetadataHosts(ctx, issuer, entry.Document); err != nil {
				return rfc8414Document{}, err
			}
			return entry.Document, nil
		}
	}
	doc, discoveryErr := attemptIssuerProbe(ctx, doer, strings.TrimSuffix(issuer.Issuer, "/")+"/.well-known/openid-configuration")
	if discoveryErr != nil {
		return rfc8414Document{}, ErrFederatedConfiguration
	}
	normalizeFederatedScopePresence(&doc)
	if err := m.validateFederatedMetadataHosts(ctx, issuer, doc); err != nil {
		return rfc8414Document{}, err
	}
	if m.locks != nil {
		// Cache write failure does not invalidate successfully validated discovery.
		_ = m.locks.Set(ctx, key, federatedMetadataCacheEntry{Document: doc, ExpiresAt: time.Now().Add(federatedMetadataTTL), ScopesOmitted: doc.ScopesSupported == nil}, federatedMetadataTTL)
	}
	return doc, nil
}

// normalizeFederatedScopePresence applies offline-access policy only to OIDC
// login discovery. Shared OAuth discovery retains its existing null semantics.
// The probe has already validated JSON and retains the original wire document.
func normalizeFederatedScopePresence(doc *rfc8414Document) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(doc.raw, &fields) == nil {
		if _, present := fields["scopes_supported"]; present && doc.ScopesSupported == nil {
			doc.ScopesSupported = []string{}
		}
	}
}

func (entry *federatedMetadataCacheEntry) restoreScopePresence() {
	if entry.ScopesOmitted {
		entry.Document.ScopesSupported = nil
	} else if entry.Document.ScopesSupported == nil {
		entry.Document.ScopesSupported = []string{}
	}
}
