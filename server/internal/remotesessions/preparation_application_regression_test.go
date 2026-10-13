package remotesessions_test

import (
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"testing"
	"time"

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
		OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), Slug: "inherited-publication", Issuer: "https://issuer.example.com", TokenEndpoint: conv.ToPGText("https://issuer.example.com/token"), ScopesSupported: []string{"openid"}, GrantTypesSupported: []string{oauthwire.GrantTypeJWTBearer}, AuthorizationGrantProfilesSupported: []string{"urn:ietf:params:oauth:grant-profile:id-jag"}, ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		MetadataFetchedAt: conv.ToPGTimestamptz(time.Now()),
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

func TestPreparationInheritedClientGrantChangeRequiresOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), Slug: "inherited-unchanged", Issuer: "https://issuer.example.com", TokenEndpoint: conv.ToPGText("https://issuer.example.com/token"), MetadataFetchedAt: conv.ToPGTimestamptz(time.Now()), ScopesSupported: []string{"openid"}, GrantTypesSupported: []string{oauthwire.GrantTypeJWTBearer}, AuthorizationGrantProfilesSupported: []string{"urn:ietf:params:oauth:grant-profile:id-jag"}, ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	in.RemoteSessionIssuerID = issuer.ID
	in.ClientID = seedOrgLevelRemoteClient(t, ctx, ti.conn, auth.ActiveOrganizationID, in.RemoteSessionIssuerID, "inherited-unchanged-client")
	existing := []string{"authorization_code"}
	_, err = q.SetEMAClientGrants(ctx, repo.SetEMAClientGrantsParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), GrantTypes: existing})
	require.NoError(t, err)
	projectWrite := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, auth.ProjectID.String())})
	clientGrants := func() []string {
		row, err := q.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{ID: in.ClientID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
		require.NoError(t, err)
		return row.RemoteSessionClient.GrantTypes
	}

	in.ConfirmGrants = []string{"authorization_code", "refresh_token"}
	_, err = ti.service.PrepareIdentityChaining(projectWrite, in)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, existing, clientGrants(), "refused grant change must not rewrite the org-owned client")

	in.ConfirmGrants = existing
	result, err := ti.service.PrepareIdentityChaining(projectWrite, in)
	require.NoError(t, err, "confirming unchanged grants does not need org:admin")
	require.Equal(t, "manual_setup_required", result.State)
	require.Equal(t, existing, clientGrants())

	orgAdmin := withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, auth.ProjectID.String())},
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, auth.ActiveOrganizationID)})
	in.ConfirmGrants = []string{"authorization_code", "refresh_token"}
	in.ExpectedGeneration = result.Generation
	_, err = ti.service.PrepareIdentityChaining(orgAdmin, in)
	require.NoError(t, err)
	require.ElementsMatch(t, in.ConfirmGrants, clientGrants())
}

func TestPreparationFixtureRegistrationScopesJoinedClient(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	params := testrepo.GetPreparationFixtureRegistrationParams{ID: prepared.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID}
	_, err = testrepo.New(ti.conn).GetPreparationFixtureRegistration(ctx, params)
	require.NoError(t, err)
	other := createProject(t, ctx, ti.conn, "fixture-other")
	foreign := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, other, in.RemoteSessionIssuerID, "foreign-client")
	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: prepared.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ExpectedGeneration: prepared.Generation, Generation: prepared.Generation + 1, State: conv.ToPGText("unknown_grants"), GrantSource: conv.ToPGText("unknown"), RemoteSessionClientID: conv.ToNullUUID(foreign), RequestedScopes: in.Scopes})
	require.ErrorIs(t, err, pgx.ErrNoRows, "explicit conditional write rejects the foreign reference")
	registration, err := testrepo.New(ti.conn).GetPreparationFixtureRegistration(ctx, params)
	require.NoError(t, err)
	require.Equal(t, in.ClientID, registration.RemoteSessionClientID.UUID, "rejected writes preserve the authorized registration")
	params.ProjectID = uuid.New()
	_, err = testrepo.New(ti.conn).GetPreparationFixtureRegistration(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestPreparationRejectedChangesReturnPersistedBinding(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"explicit_client_dcr", "unsupported_cimd", "unsupported_scopes", "missing_selection", "unsupported_dcr"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			if scenario == "unsupported_scopes" {
				_, err := repo.New(ti.conn).UpdateRemoteSessionClient(ctx, repo.UpdateRemoteSessionClientParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), Scope: []string{"openid"}})
				require.NoError(t, err)
			}
			preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer})
			prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, "ready", prepared.State)
			in.ExpectedGeneration = prepared.Generation
			if scenario == "missing_selection" || scenario == "unsupported_dcr" {
				prepared, err = ti.service.UnlinkIdentityChaining(ctx, in)
				require.NoError(t, err)
				in.ExpectedGeneration = prepared.Generation
				in.ClientID = uuid.Nil
			}
			q := repo.New(ti.conn)
			key := repo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource}
			before, err := q.GetEMABinding(ctx, key)
			require.NoError(t, err)
			in.Scopes = []string{"unconfigured-scope"}
			want := "manual_setup_required"
			switch scenario {
			case "explicit_client_dcr":
				in.Mechanism = "dcr"
				want = "configuration_required"
			case "unsupported_cimd":
				in.Mechanism = "cimd"
			case "missing_selection":
				in.Mechanism = "manual"
				want = "configuration_required"
			case "unsupported_dcr":
				in.Mechanism = "dcr"
				in.TokenEndpointAuthMethod = "none"
			}
			rejected, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, want, rejected.State)
			require.Equal(t, before.Generation, rejected.Generation)
			require.Equal(t, before.RequestedScopes, rejected.Scopes)
			after, err := q.GetEMABinding(ctx, key)
			require.NoError(t, err)
			require.Equal(t, before, after, "rejected changes must not mutate the durable binding")
		})
	}
}
