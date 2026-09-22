package remotesessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jackc/pgx/v5/pgxpool"
	keyrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Key rotation under the same set ID is a relevant credential revision, unlike
// a display-name edit. This reads only public key identifiers and pinned version
// metadata; it never contacts the KMS, decrypts secrets or exercises credentials.
func federatedSigningKeyRevision(ctx context.Context, db *pgxpool.Pool, organizationID string, client repo.RemoteSessionClient) (string, error) {
	if client.TokenEndpointAuthMethod.String != string(TokenEndpointAuthMethodPrivateKeyJWT) {
		return "", nil
	}
	if !client.JsonWebKeySetID.Valid {
		return "", ErrFederatedConfiguration
	}
	q := keyrepo.New(db)
	if _, err := q.GetJsonWebKeySet(ctx, keyrepo.GetJsonWebKeySetParams{ID: client.JsonWebKeySetID.UUID, OrganizationID: organizationID}); err != nil {
		return "", federatedSigningRevisionError(err)
	}
	key, err := q.GetActiveJsonWebKey(ctx, keyrepo.GetActiveJsonWebKeyParams{JsonWebKeySetID: client.JsonWebKeySetID.UUID, OrganizationID: organizationID})
	if err != nil {
		return "", federatedSigningRevisionError(err)
	}
	encoded, err := json.Marshal(federatedSigningKeyRevisionInput{ID: key.ID.String(), ExternalKeyID: key.ExternalKeyID.String(), Version: key.ExternalKeyVersion.String, KeyID: key.Kid})
	if err != nil {
		return "", ErrFederatedConfiguration
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func federatedSigningRevisionError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFederatedConfiguration
	}
	return fmt.Errorf("read federated signing key revision: %w", err)
}

// LoadFederatedDelegationProvider reads only the current registration and
// signing-key revision. Retained assertions do not depend on discovery, DNS,
// upstream availability, or private signing-key access. A refresh must load the
// full provider separately before making an upstream request.
func (m *ChallengeManager) LoadFederatedDelegationProvider(ctx context.Context, organizationID string, issuerID, clientID uuid.UUID) (*FederatedProvider, error) {
	if organizationID == "" || issuerID == uuid.Nil || clientID == uuid.Nil {
		return nil, ErrFederatedConfiguration
	}
	row, err := repo.New(m.db).GetTrustedRemoteSessionClientForOrganization(ctx, repo.GetTrustedRemoteSessionClientForOrganizationParams{
		OrganizationID: organizationID, IssuerID: issuerID, ClientID: clientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrFederatedConfiguration
		}
		return nil, fmt.Errorf("read federated delegation registration: %w", err)
	}
	var metadata rfc8414Document
	p := &FederatedProvider{metadata: metadata, fingerprint: "", signingKeyRevision: "", organizationID: organizationID, issuer: row.RemoteSessionIssuer, client: row.RemoteSessionClient}
	p.signingKeyRevision, err = federatedSigningKeyRevision(ctx, m.db, organizationID, p.client)
	if err != nil {
		return nil, err
	}
	if p.DelegationConfigurationHash() == "" {
		return nil, ErrFederatedConfiguration
	}
	return p, nil
}

// federatedSigningKeyRevisionInput identifies the active public signing key.
// Its JSON field names and order are stable hash inputs, not an external protocol.
// Changing the encoding invalidates stored delegation configuration revisions.
type federatedSigningKeyRevisionInput struct {
	ID            string `json:"ID"`
	ExternalKeyID string `json:"ExternalKeyID"`
	Version       string `json:"Version"`
	KeyID         string `json:"KeyID"`
}
