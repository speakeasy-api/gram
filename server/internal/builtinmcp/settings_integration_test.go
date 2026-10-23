package builtinmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestSettingsStoreIntegration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	container, clone, err := testenv.NewTestPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	db, err := clone(t, "builtin_settings")
	require.NoError(t, err)
	const org = "org_builtin_test"
	const user = "user_builtin_admin"
	orgs := orgrepo.New(db)
	_, err = orgs.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: org, Name: "Example", Slug: "builtin-example"})
	require.NoError(t, err)
	projects := projectrepo.New(db)
	first, err := projects.CreateProject(ctx, projectrepo.CreateProjectParams{OrganizationID: org, Name: "First", Slug: "first"})
	require.NoError(t, err)
	second, err := projects.CreateProject(ctx, projectrepo.CreateProjectParams{OrganizationID: org, Name: "Second", Slug: "second"})
	require.NoError(t, err)
	project, otherProject := first.ID, second.ID
	_, err = usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: user, Email: "admin@example.invalid", DisplayName: "Admin"})
	require.NoError(t, err)
	require.NoError(t, orgs.AttachWorkOSUserToOrg(ctx, orgrepo.AttachWorkOSUserToOrgParams{OrganizationID: org, UserID: conv.ToPGText(user)}))
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, db, org))
	roles := accessrepo.New(db)
	_, err = roles.UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{OrganizationID: org, UserID: conv.ToPGText(user), WorkosUserID: "external_test", WorkosRoleSlug: "admin", WorkosUpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
	require.NoError(t, err)
	environments := envrepo.New(db)
	firstEnv, err := environments.CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{OrganizationID: org, ProjectID: project, Name: "Default", Slug: "default"})
	require.NoError(t, err)
	secondEnv, err := environments.CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{OrganizationID: org, ProjectID: otherProject, Name: "Other", Slug: "default"})
	require.NoError(t, err)
	environment, otherEnvironment := firstEnv.ID, secondEnv.ID

	ac := &contextvalues.AuthContext{ActiveOrganizationID: org, UserID: user, ProjectID: &project}
	admin := contextvalues.WithAuthenticatedActor(ctx, ac, urn.NewPrincipal(urn.PrincipalTypeUser, user))
	store := NewStore(db, audit.NewLogger(), nil)

	initial, err := store.Get(admin, org, project, Slack)
	require.NoError(t, err)
	require.Equal(t, "0", initial.Version)
	require.False(t, initial.Hidden)
	require.Nil(t, initial.EnvironmentID)
	_, err = repo.New(db).GetSettings(ctx, repo.GetSettingsParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, BuiltinKey: Slack})
	require.ErrorIs(t, err, pgx.ErrNoRows, "discovery must not provision records")

	updated, err := store.Update(admin, org, project, Slack, Update{Hidden: true, EnvironmentID: uuid.NullUUID{UUID: environment, Valid: true}, ExpectedVersion: "0"})
	require.NoError(t, err)
	require.True(t, updated.Hidden)
	require.Equal(t, "1", updated.Version)
	require.Equal(t, initial.ID, updated.ID)
	require.Equal(t, initial.URL, updated.URL)
	require.NotNil(t, updated.EnvironmentID)
	listed, err := store.List(admin, org, project)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.True(t, listed[0].Hidden, "settings must retain hidden entries")
	logs, err := auditrepo.New(db).ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, Action: conv.ToPGText("builtin_mcp:update")})
	require.NoError(t, err)
	require.Len(t, logs, 1)

	_, err = store.Update(admin, org, project, Slack, Update{ExpectedVersion: "0"})
	require.Error(t, err, "stale mutation must fail")
	_, err = store.Update(admin, org, project, Slack, Update{EnvironmentID: uuid.NullUUID{UUID: otherEnvironment, Valid: true}, ExpectedVersion: "1"})
	require.Error(t, err, "cross-project environment must fail")
	_, err = store.Get(admin, "org_wrong", project, Slack)
	require.Error(t, err, "wrong organization must fail")

	reset, err := store.Update(admin, org, project, Slack, Update{ExpectedVersion: "1"})
	require.NoError(t, err)
	require.Equal(t, "2", reset.Version)
	require.False(t, reset.Hidden)
	require.Nil(t, reset.EnvironmentID)
	// A prepared admin grant never substitutes for the live canonical role.
	_, err = roles.SoftDeleteAllRoleAssignmentsByWorkosUser(ctx, accessrepo.SoftDeleteAllRoleAssignmentsByWorkosUserParams{OrganizationID: org, WorkosUserID: "external_test"})
	require.NoError(t, err)
	prepared := authz.GrantsToContext(admin, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, org)})
	_, err = store.Get(prepared, org, project, Slack)
	require.Error(t, err, "revoked live admin role must reject stale grants")
}
