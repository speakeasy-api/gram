package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// clientTemplate is what a new client takes from the stored client it is
// created from: the upstream registration, never anything a person or an
// agent did through it. The new client is its own row, so sessions, grants,
// user session issuer attachments and MCP server associations stay with the
// template, and the template row is only read.
type clientTemplate struct {
	// sourceID is the template client.
	sourceID uuid.UUID

	// secretEncrypted is the template's stored secret ciphertext, invalid for
	// a public client. It moves between rows without being decrypted.
	secretEncrypted pgtype.Text

	// clientIDIssuedAt and clientSecretExpiresAt are the registration's
	// lifecycle stamps, which belong to the credential rather than the row.
	clientIDIssuedAt      pgtype.Timestamptz
	clientSecretExpiresAt pgtype.Timestamptz

	// legacyCallbackURL and callbackBaseURL locate the redirect URI the
	// upstream registration lists. A client created now would otherwise
	// record today's callback origin, which the provider was never given.
	legacyCallbackURL bool
	callbackBaseURL   pgtype.Text
}

// loadClientTemplate reads the template a new client of issuerID with
// clientID is created from, as visible to the project, and share-locks its
// row: inside a transaction a rotated or rejected template cannot be copied
// stale. A refusal is an *IdentityError whose message is safe to show the
// caller.
//
// The stored secret belongs to one upstream registration, so it is only
// handed to a client with the template's own issuer and client_id. Templates
// whose credentials cannot stand in a second row are refused:
//
//   - a client metadata document client, whose client_id is its own hosted
//     document;
//   - a private_key_jwt client, which the provider knows by the template's
//     key set address;
//   - a credential_owner self client, which is a shared credential rather
//     than a personal sign-in;
//   - a client managed by an identity provider connection, whose secret
//     belongs to that connection;
//   - a client the issuer has rejected, whose credentials no longer work.
//
// A template whose stored secret has expired is refused by requireSecret,
// unless the caller supplies a secret of its own.
func loadClientTemplate(ctx context.Context, q *repo.Queries, projectID uuid.UUID, organizationID string, templateID, issuerID uuid.UUID, clientID string) (clientTemplate, error) {
	source, err := q.LockRemoteSessionClientTemplate(ctx, repo.LockRemoteSessionClientTemplateParams{
		ID:             templateID,
		ProjectID:      projectID,
		OrganizationID: conv.ToPGText(organizationID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return clientTemplate{}, identityRefusal(ErrIdentityNotFound, err, "template remote session client not found")
	}
	if err != nil {
		return clientTemplate{}, fmt.Errorf("get template remote session client: %w", err)
	}

	switch {
	case source.RemoteSessionIssuerID != issuerID:
		return clientTemplate{}, identityRefusal(ErrIdentityInvalid, nil, "the template client belongs to a different Remote Identity Provider")
	case source.ClientIDMetadataUri.Valid:
		return clientTemplate{}, identityRefusal(ErrIdentityInvalid, nil, "a client metadata document client has no credentials to copy; register a new client instead")
	case TokenEndpointAuthMethod(source.TokenEndpointAuthMethod.String) == TokenEndpointAuthMethodPrivateKeyJWT:
		return clientTemplate{}, identityRefusal(ErrIdentityInvalid, nil, "a client that signs in with a key set cannot be used as a template, because the identity provider only knows the original client's key set address; create a new client instead")
	case CredentialOwner(source.CredentialOwner) == CredentialOwnerSelf:
		return clientTemplate{}, identityRefusal(ErrIdentityInvalid, nil, "a client that signs in as itself cannot be used as a template for a personal sign-in")
	case source.IdentityProviderConnectionID.Valid:
		return clientTemplate{}, identityRefusal(ErrIdentityInvalid, nil, "this client is managed by an identity provider connection and cannot be used as a template")
	case source.UpstreamRejectedAt.Valid:
		return clientTemplate{}, identityRefusal(ErrIdentityInvalid, nil, "the identity provider no longer accepts the template client's credentials; create a new client instead")
	case strings.TrimSpace(clientID) != source.ClientID:
		return clientTemplate{}, identityRefusal(ErrIdentityInvalid, nil, "a template's stored secret is only reused with its own client_id; omit template_client_id and supply the credentials for a different client_id")
	}

	return clientTemplate{
		sourceID:              source.ID,
		secretEncrypted:       source.ClientSecretEncrypted,
		clientIDIssuedAt:      source.ClientIDIssuedAt,
		clientSecretExpiresAt: source.ClientSecretExpiresAt,
		legacyCallbackURL:     source.LegacyCallbackUrl,
		callbackBaseURL:       source.CallbackBaseUrl,
	}, nil
}

// requireSecret refuses a copy that could not authenticate: a stored secret
// that has expired, or a secret-based token endpoint method when neither the
// caller nor the template supplies a secret.
func (t clientTemplate) requireSecret(method string, suppliedSecret bool) error {
	if suppliedSecret {
		return nil
	}
	if t.secretEncrypted.Valid {
		if t.clientSecretExpiresAt.Valid && !t.clientSecretExpiresAt.Time.After(time.Now()) {
			return identityRefusal(ErrIdentityInvalid, nil, "the template client's stored secret has expired; enter a current client_secret instead of copying it")
		}
		return nil
	}
	if method == string(TokenEndpointAuthMethodBasic) || method == string(TokenEndpointAuthMethodPost) {
		return identityRefusal(ErrIdentityInvalid, nil, method+" requires client_secret, and the template client stores none")
	}
	return nil
}
