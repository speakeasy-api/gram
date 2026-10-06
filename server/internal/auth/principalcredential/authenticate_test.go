package principalcredential_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// seedTenant creates an organization, a project, and members who can read it.
func seedTenant(t *testing.T, db *pgxpool.Pool, users ...string) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: "org-test", Name: "Credential tests", Slug: "credential-tests", WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}, CreationSource: pgtype.Text{}})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Credentials", Slug: "credentials", OrganizationID: "org-test"})
	require.NoError(t, err)
	selector, err := authz.NewSelector(authz.ScopeProjectRead, project.ID.String()).MarshalJSON()
	require.NoError(t, err)
	for _, user := range users {
		_, err = usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: user, Email: user + "@example.invalid", DisplayName: user, PhotoUrl: pgtype.Text{}, Admin: false})
		require.NoError(t, err)
		_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: "org-test", UserID: pgtype.Text{String: user, Valid: true}})
		require.NoError(t, err)
		_, err = accessrepo.New(db).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: "org-test", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, user), Scope: string(authz.ScopeProjectRead), Selectors: selector})
		require.NoError(t, err)
	}
	return project.ID
}

func mintFor(t *testing.T, issuer *principalcredential.Issuer, project uuid.UUID, authorizer string) string {
	t.Helper()
	raw, _, err := issuer.Mint(principalcredential.Credential{
		OrganizationID: "org-test", ProjectID: project, AuthorizerUserID: authorizer,
		Principal: urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()),
		Grants:    []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, uuid.NewString())},
	})
	require.NoError(t, err)
	return raw
}

func TestAuthenticateRechecksTheAuthorizer(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "principal_credential_authorizer")
	require.NoError(t, err)
	project := seedTenant(t, db, "user-1", "user-2")
	issuer := principalcredential.New(newSigner(t), db)

	ctx, err := issuer.Authenticate(t.Context(), mintFor(t, issuer, project, "user-1"))
	require.NoError(t, err)
	credential, ok := contextvalues.PrincipalCredentialAuthorization(ctx)
	require.True(t, ok)
	require.Equal(t, "user-1", credential.AuthorizerUserID)

	require.NoError(t, orgrepo.New(db).DeleteOrganizationUserRelationship(t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: "org-test", UserID: pgtype.Text{String: "user-2", Valid: true}}))
	_, err = issuer.Authenticate(t.Context(), mintFor(t, issuer, project, "user-2"))
	require.ErrorIs(t, err, principalcredential.ErrInvalid, "an authorizer who left the organization stops vouching for the credential")

	other, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Other", Slug: "other", OrganizationID: "org-test"})
	require.NoError(t, err)
	_, err = issuer.Authenticate(t.Context(), mintFor(t, issuer, other.ID, "user-1"))
	require.ErrorIs(t, err, principalcredential.ErrInvalid, "an authorizer who cannot read the project cannot back a credential for it")
}

func TestAuthenticateRejectsDisabledOrganizations(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "principal_credential_disabled")
	require.NoError(t, err)
	project := seedTenant(t, db, "user-1")
	issuer := principalcredential.New(newSigner(t), db)
	raw := mintFor(t, issuer, project, "user-1")
	_, err = issuer.Authenticate(t.Context(), raw)
	require.NoError(t, err)

	_, err = adminrepo.New(db).AdminDisableOrganization(t.Context(), "org-test")
	require.NoError(t, err)
	_, err = issuer.Authenticate(t.Context(), raw)
	require.ErrorIs(t, err, principalcredential.ErrInvalid)
}

func TestAuthenticateRevalidatesADifferentToken(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "principal_credential_second_token")
	require.NoError(t, err)
	project := seedTenant(t, db, "user-1", "user-2")
	issuer := principalcredential.New(newSigner(t), db)

	first, err := issuer.Authenticate(t.Context(), mintFor(t, issuer, project, "user-1"))
	require.NoError(t, err)
	again, err := issuer.Authenticate(first, mintFor(t, issuer, project, "user-2"))
	require.NoError(t, err)
	credential, ok := contextvalues.PrincipalCredentialAuthorization(again)
	require.True(t, ok)
	require.Equal(t, "user-2", credential.AuthorizerUserID, "a different token is authenticated on its own")

	_, err = issuer.Authenticate(first, "not-a-credential")
	require.ErrorIs(t, err, principalcredential.ErrNotCredential, "an existing credential never vouches for another value")
	foreign := mintFor(t, principalcredential.New(newSigner(t), db), project, "user-1")
	_, err = issuer.Authenticate(first, foreign)
	require.ErrorIs(t, err, principalcredential.ErrInvalid)
}
