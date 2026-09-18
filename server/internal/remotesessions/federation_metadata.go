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
	Document  rfc8414Document
	ExpiresAt time.Time
}

// updated_at changes on JWKS cache writes as well as administrator edits. Bind
// the effective configuration instead, excluding only cache state/timestamps.
// Ownership, attachment tier, stored metadata/policies and tunnel are included.
func federatedIssuerVersion(issuer repo.RemoteSessionIssuer) string {
	issuer.Jwks = nil
	issuer.JwksFetchedAt = pgtype.Timestamptz{}
	issuer.JwksCacheExpiresAt = pgtype.Timestamptz{}
	issuer.JwksLastErrorAt = pgtype.Timestamptz{}
	issuer.JwksLastError = pgtype.Text{}
	issuer.JwksEtag = pgtype.Text{}
	issuer.MetadataFetchedAt = pgtype.Timestamptz{}
	issuer.MetadataLastErrorAt = pgtype.Timestamptz{}
	issuer.MetadataLastError = pgtype.Text{}
	issuer.MetadataLastErrorUrl = pgtype.Text{}
	issuer.UpdatedAt = pgtype.Timestamptz{}
	encoded, _ := json.Marshal(issuer)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func federatedMetadataCacheKey(organizationID string, issuer repo.RemoteSessionIssuer) string {
	encoded, _ := json.Marshal([]string{organizationID, issuer.ID.String(), issuer.Issuer, federatedIssuerVersion(issuer)})
	sum := sha256.Sum256(encoded)
	return "federated-oidc-metadata:v1:" + hex.EncodeToString(sum[:])
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
	if err := m.validateFederatedMetadataHosts(ctx, issuer, doc); err != nil {
		return rfc8414Document{}, err
	}
	if m.locks != nil {
		// Cache write failure does not invalidate successfully validated discovery.
		_ = m.locks.Set(ctx, key, federatedMetadataCacheEntry{Document: doc, ExpiresAt: time.Now().Add(federatedMetadataTTL)}, federatedMetadataTTL)
	}
	return doc, nil
}
