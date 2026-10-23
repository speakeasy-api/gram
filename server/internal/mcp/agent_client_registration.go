package mcp

import (
	"context"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// RegisterAgentOAuthClient is the authenticated registration mechanism shared
// by the public resource registration endpoint and trusted runtime composition.
// Callers must establish an agent actor and its immutable authorizer first.
// No assistant, trigger, workload issuer, or runtime row is required.
func RegisterAgentOAuthClient(ctx context.Context, db usersessions_repo.DBTX, target AgentAuthorizationTarget, request usersessions.RegistrationRequest) (usersessions_repo.UserSessionClient, error) {
	var zero usersessions_repo.UserSessionClient
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	ac, hasAuth := contextvalues.GetAuthContext(ctx)
	credential, hasCredential := contextvalues.PrincipalCredentialAuthorization(ctx)
	if !ok || actor.Type != urn.PrincipalTypeAgent || !hasAuth || ac == nil || ac.ActiveOrganizationID != target.OrganizationID || !hasCredential {
		return zero, oops.C(oops.CodeUnauthorized)
	}
	if request.TokenEndpointAuthMethod != "private_key_jwt" || len(request.GrantTypes) != 1 || request.GrantTypes[0] != "client_credentials" {
		return zero, oops.C(oops.CodeBadRequest)
	}
	request.SetDefaults()
	if err := request.Validate([]string{"client_credentials"}, []string{"private_key_jwt"}); err != nil {
		return zero, fmt.Errorf("register agent OAuth client: %w", err)
	}
	admission, err := runtimepolicy.AdmitPrincipalCredentialWithDBTX(ctx, db)
	if err != nil {
		return zero, fmt.Errorf("register agent OAuth client: %w", err)
	}
	for _, grants := range [][]authz.Grant{admission.Credential, admission.Agent, admission.Owner, admission.Authorizer.Policy} {
		allowed, err := authz.GrantsAuthorize(grants, target.connectCheck())
		if err != nil || !allowed {
			return zero, oops.C(oops.CodeForbidden)
		}
	}
	profile, err := newAgentSessionCredential(target, credential.AuthorizerUserID)
	if err != nil {
		return zero, fmt.Errorf("register agent OAuth client: %w", err)
	}
	client, err := usersessions_repo.New(db).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		ClientSecretHash:      pgtype.Text{String: "", Valid: false},
		ClientSecretExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		UserSessionIssuerID:   target.UserSessionIssuerID, ClientID: "client_" + uuid.NewString(), ClientName: request.ClientName, RedirectUris: []string{}, TokenEndpointAuthMethod: oauthwire.AuthMethodPrivateKeyJWT, ClientJwks: request.JWKS, ClientJwksUri: pgtype.Text{String: request.JWKSURI, Valid: request.JWKSURI != ""},
		TrustedSubjectUrn: pgtype.Text{String: actor.String(), Valid: true}, AuthorizerUserID: pgtype.Text{String: profile.AuthorizerUserID, Valid: true}, DelegatedGrants: profile.DelegatedGrants, DelegatedGrantsVersion: pgtype.Int4{Int32: profile.DelegatedGrantsVersion, Valid: true},
	})
	if err != nil {
		return zero, fmt.Errorf("persist agent OAuth client: %w", err)
	}
	return client, nil
}
