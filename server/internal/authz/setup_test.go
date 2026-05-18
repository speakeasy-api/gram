package authz

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testinfra"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var (
	cloneTestDatabase   testinfra.PostgresDBCloneFunc
	newClickhouseClient testinfra.ClickhouseClientFunc
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, cloneFunc, err := testinfra.NewTestPostgres(ctx)
	if err != nil {
		log.Fatalf("launch test postgres: %v", err)
	}
	cloneTestDatabase = cloneFunc

	chContainer, chFactory, err := testinfra.NewTestClickhouse(ctx)
	if err != nil {
		log.Fatalf("launch test clickhouse: %v", err)
	}
	newClickhouseClient = chFactory

	code := m.Run()

	if err := chContainer.Terminate(ctx); err != nil {
		log.Fatalf("terminate clickhouse container: %v", err)
	}
	if err := pgContainer.Terminate(ctx); err != nil {
		log.Fatalf("terminate postgres container: %v", err)
	}

	os.Exit(code)
}

func enterpriseTestCtx(ctx context.Context) context.Context {
	sessionID := "session_test"
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_test",
		UserID:                "user_test",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})
}

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	conn, err := cloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	return conn
}

func seedOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     "Test Org",
		Slug:     "test-org",
		WorkosID: conv.PtrToPGText(conv.PtrEmpty("workos-org-" + organizationID)),
	})
	require.NoError(t, err)
}

func seedGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope Scope, resource string) {
	t.Helper()

	selectors, err := NewSelector(scope, resource).MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func seedGrantWithSelector(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope Scope, sel Selector) {
	t.Helper()

	selectors, err := sel.MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func seedConnectedUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, userID string, email string, displayName string, workosUserID string, workosMembershipID string) {
	t.Helper()

	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: displayName,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	err = usersrepo.New(conn).SetUserWorkosID(ctx, usersrepo.SetUserWorkosIDParams{
		WorkosID: conv.PtrToPGText(conv.PtrEmpty(workosUserID)),
		ID:       userID,
	})
	require.NoError(t, err)

	err = orgrepo.New(conn).AttachWorkOSUserToOrg(ctx, orgrepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     organizationID,
		UserID:             userID,
		WorkosMembershipID: conv.PtrToPGText(conv.PtrEmpty(workosMembershipID)),
	})
	require.NoError(t, err)
}

func seedRole(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, slug string) string {
	t.Helper()

	now := time.Now().UTC()
	role, err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        slug,
		WorkosName:        slug,
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	return "role:organization:" + role.ID.String()
}

func seedRoleAssignment(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, userID string, workosUserID string, workosMembershipID string, roleSlug string) {
	t.Helper()

	seedRole(t, ctx, conn, organizationID, roleSlug)
	rows, err := accessrepo.New(conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     organizationID,
		WorkosUserID:       workosUserID,
		UserID:             conv.ToPGTextEmpty(userID),
		WorkosMembershipID: conv.ToPGTextEmpty(workosMembershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		WorkosRoleSlug:     roleSlug,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}
