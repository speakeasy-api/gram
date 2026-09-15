package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestPreparationInheritedGrantPublicationPreservesForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer, err := repo.New(ti.conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), Slug: "inherited-publication", Issuer: "https://issuer.example.com", TokenEndpoint: conv.ToPGText("https://issuer.example.com/token"), ScopesSupported: []string{"openid"}, GrantTypesSupported: []string{preparationJWTGrant}, AuthorizationGrantProfilesSupported: []string{"urn:ietf:params:oauth:grant-profile:id-jag"}, ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	in.RemoteSessionIssuerID = issuer.ID
	in.ClientID = seedOrgLevelRemoteClient(t, ctx, ti.conn, auth.ActiveOrganizationID, in.RemoteSessionIssuerID, "inherited-client")
	// Non-EMA grants still publish registration metadata without requiring a secret.
	in.ConfirmGrants = []string{"authorization_code"}
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, auth.ProjectID.String())})
	_, err = ti.service.PrepareIdentityChaining(ctx, in)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, auth.ProjectID.String())},
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, auth.ActiveOrganizationID)})
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "manual_setup_required", result.State)
	require.Equal(t, in.ConfirmGrants, result.GrantTypes)
}

func TestPreparationFixtureRegistrationScopesJoinedClient(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	params := repo.GetPreparationFixtureRegistrationParams{ID: prepared.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID}
	_, err = q.GetPreparationFixtureRegistration(ctx, params)
	require.NoError(t, err)
	other := createProject(t, ctx, ti.conn, "fixture-other")
	foreign := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, other, in.RemoteSessionIssuerID, "foreign-client")
	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: prepared.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ExpectedGeneration: prepared.Generation, Generation: prepared.Generation + 1, State: "unknown_grants", GrantSource: "unknown", RemoteSessionClientID: conv.ToNullUUID(foreign), RequestedScopes: in.Scopes})
	require.NoError(t, err)
	_, err = q.GetPreparationFixtureRegistration(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	params.ProjectID = uuid.New()
	_, err = q.GetPreparationFixtureRegistration(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
