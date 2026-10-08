package identityproviderconnections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// SignInScopes are the scopes the dashboard's Okta sign-in setup requests.
var SignInScopes = []string{"openid", "email", "profile", "offline_access"}

// Sign-in next steps, in setup order.
const (
	SignInStepVerifyConnection  = "verify_connection"
	SignInStepRecordAgent       = "record_ai_agent"
	SignInStepAwaitProvision    = "await_provisioning"
	SignInStepResolveDuplicates = "resolve_duplicate_sign_in_clients"
	SignInStepSetUpSignIn       = "set_up_sign_in"
	SignInStepTrustSignIn       = "trust_sign_in"
	SignInStepRegisterInOkta    = "register_in_okta"
)

// SignInIssuer is an organization sign-in issuer that trusts a sign-in client of the connection's issuer.
type SignInIssuer struct {
	ID   string
	Slug string
}

// SignInSetup is what Speakeasy's records say about Okta sign-in. Okta-side
// configuration (the agent's public key, the linked app's grants and redirect
// URI) is never observed.
type SignInSetup struct {
	ConnectionStatus string
	AgentRecorded    bool
	ClientRegistered bool
	// More than one when several clients claim the agent ID; none is then chosen.
	DuplicateClients int
	// ClientReady mirrors the dashboard: private_key_jwt with the token
	// endpoint audience, a key set, and every sign-in scope.
	ClientReady bool
	RedirectURI string
	JWKSURL     string
	ActiveKeyID string
	// The active signing key's public members only.
	PublicJWK       map[string]any
	TrustingIssuers []SignInIssuer
	// Issuers trusting a sign-in client for another agent ID, or a deleted one.
	StaleTrustingIssuers []SignInIssuer
	Checklist            []ChecklistItem
	NextStep             string
}

// SignInReader reads Okta sign-in setup without a provisioner. Callers authorize.
type SignInReader struct {
	logger  *slog.Logger
	db      repo.DBTX
	origins remotesessions.CallbackOrigins
}

func NewSignInReader(logger *slog.Logger, db repo.DBTX, origins remotesessions.CallbackOrigins) *SignInReader {
	return &SignInReader{logger: logger, db: db, origins: origins}
}

// Read returns ErrConnectionNotFound when the organization has no live Okta connection.
func (r *SignInReader) Read(ctx context.Context, organizationID string) (*SignInSetup, error) {
	q := repo.New(r.db)
	row, err := q.GetOktaIdentityProviderConnection(ctx, repo.GetOktaIdentityProviderConnectionParams{
		OrganizationID: organizationID,
		ID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrConnectionNotFound
	case err != nil:
		return nil, fmt.Errorf("load okta connection: %w", err)
	}

	rows := connectionRows{Connection: row.IdentityProviderConnection, Okta: row.OktaIdentityProviderConnection, Managed: nil}
	managedRow, err := q.GetManagedClient(ctx, repo.GetManagedClientParams{
		OrganizationID:               conv.ToPGText(organizationID),
		IdentityProviderConnectionID: conv.ToNullUUID(row.IdentityProviderConnection.ID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("load managed client: %w", err)
	default:
		rows.Managed = managedClientFromRow(managedRow, r.origins.Outbound)
	}

	agentID := row.OktaIdentityProviderConnection.AgentID.String
	setup := &SignInSetup{
		ConnectionStatus:     rows.Connection.Status,
		AgentRecorded:        agentID != "",
		ClientRegistered:     false,
		DuplicateClients:     0,
		ClientReady:          false,
		RedirectURI:          "",
		JWKSURL:              "",
		ActiveKeyID:          "",
		PublicJWK:            nil,
		TrustingIssuers:      []SignInIssuer{},
		StaleTrustingIssuers: []SignInIssuer{},
		Checklist:            nil,
		NextStep:             "",
	}
	missing := []string{}
	if rows.checked() {
		missing = missingScopes(rows.Okta.GrantedScopes)
	}
	setup.Checklist = rows.checklist(missing, observeAgent(ctx, r.logger, r.db, rows))

	if setup.AgentRecorded && rows.Managed != nil {
		if err := r.readClient(ctx, q, organizationID, rows.Managed.IssuerID, agentID, setup); err != nil {
			return nil, err
		}
	}
	setup.NextStep = signInNextStep(setup, rows.Managed != nil)
	return setup, nil
}

func (r *SignInReader) readClient(ctx context.Context, q *repo.Queries, organizationID string, issuerID uuid.UUID, agentID string, setup *SignInSetup) error {
	clients, err := q.ListOktaSignInClients(ctx, repo.ListOktaSignInClientsParams{
		OrganizationID:        conv.ToPGText(organizationID),
		RemoteSessionIssuerID: issuerID,
		ClientID:              agentID,
	})
	if err != nil {
		return fmt.Errorf("list okta sign-in clients: %w", err)
	}
	setup.ClientRegistered = len(clients) > 0
	if len(clients) > 1 {
		setup.DuplicateClients = len(clients)
	}

	trusts, err := q.ListOktaSignInIssuerTrusts(ctx, repo.ListOktaSignInIssuerTrustsParams{
		OrganizationID:        conv.ToPGText(organizationID),
		RemoteSessionIssuerID: conv.ToNullUUID(issuerID),
	})
	if err != nil {
		return fmt.Errorf("list okta sign-in issuer trusts: %w", err)
	}
	for _, trust := range trusts {
		if trust.TrustedClientDeleted || trust.TrustedClientID != agentID {
			setup.StaleTrustingIssuers = append(setup.StaleTrustingIssuers, SignInIssuer{ID: trust.ID.String(), Slug: trust.Slug})
		}
	}
	if len(clients) != 1 {
		return nil
	}

	client := clients[0]
	origin := r.origins.ForClient(client.CallbackBaseUrl)
	setup.ClientReady = client.TokenEndpointAuthMethod.String == string(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT) &&
		client.TokenEndpointAuthAudienceFormat.String == string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint) &&
		client.JsonWebKeySetID.Valid &&
		!slices.ContainsFunc(SignInScopes, func(scope string) bool { return !slices.Contains(client.Scope, scope) })
	setup.RedirectURI = remotesessions.FederatedIDPCallbackURL(origin, client.ID)
	setup.JWKSURL = remotesessions.ClientJSONWebKeySetURL(origin, client.ID)
	if client.ActiveKid.Valid {
		setup.ActiveKeyID = client.ActiveKid.String
		setup.PublicJWK = r.publicJWK(ctx, client.ActivePublicJwk)
	}
	for _, trust := range trusts {
		if trust.TrustedClientRowID == client.ID && !trust.TrustedClientDeleted {
			setup.TrustingIssuers = append(setup.TrustingIssuers, SignInIssuer{ID: trust.ID.String(), Slug: trust.Slug})
		}
	}
	return nil
}

var publicJWKMembers = []string{"kid", "kty", "alg", "use", "n", "e", "crv", "x", "y"}

// publicJWK keeps only public members, so a malformed row can never leak private ones.
func (r *SignInReader) publicJWK(ctx context.Context, raw []byte) map[string]any {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		r.logger.ErrorContext(ctx, "decode okta sign-in public jwk", attr.SlogError(err))
		return nil
	}
	jwk := make(map[string]any, len(publicJWKMembers))
	for _, member := range publicJWKMembers {
		if value, ok := doc[member]; ok {
			jwk[member] = value
		}
	}
	if _, ok := jwk["use"]; !ok {
		jwk["use"] = "sig"
	}
	return jwk
}

// signInNextStep mirrors the dashboard's Okta sign-in prerequisites.
func signInNextStep(setup *SignInSetup, provisioned bool) string {
	switch {
	case setup.ConnectionStatus != StatusVerified:
		return SignInStepVerifyConnection
	case !setup.AgentRecorded:
		return SignInStepRecordAgent
	case !provisioned:
		return SignInStepAwaitProvision
	case setup.DuplicateClients > 1:
		return SignInStepResolveDuplicates
	case !setup.ClientRegistered || !setup.ClientReady:
		return SignInStepSetUpSignIn
	case len(setup.TrustingIssuers) == 0:
		return SignInStepTrustSignIn
	default:
		return SignInStepRegisterInOkta
	}
}
