package access

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	trequire "github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

type testInstance struct {
	service *Service
	conn    *pgxpool.Pool
	roles   *MockRoleProvider
}

func newTestAccessService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	trequire.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	trequire.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)

	_, err = orgrepo.New(conn).SetOrgWorkosID(ctx, orgrepo.SetOrgWorkosIDParams{
		WorkosID:       conv.PtrToPGText(conv.PtrEmpty("org_workos_test")),
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	trequire.NoError(t, err)

	roles := newMockRoleProvider(t)

	svc := NewService(logger, tracerProvider, conn, sessionManager, roles, NewManager(logger, conn, nil))

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
		roles:   roles,
	}
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

	conn, err := infra.CloneTestDatabase(t, "testdb")
	trequire.NoError(t, err)

	return conn
}

func seedOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:              organizationID,
		Name:            "Test Org",
		Slug:            "test-org",
		SsoConnectionID: conv.PtrToPGText(nil),
	})
	trequire.NoError(t, err)
}

func seedGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope Scope, resource string) {
	t.Helper()

	_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Resource:       resource,
	})
	trequire.NoError(t, err)
}

func listPrincipalGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal) []accessrepo.PrincipalGrant {
	t.Helper()

	grants, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal.String(),
	})
	trequire.NoError(t, err)

	return grants
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
	trequire.NoError(t, err)

	err = usersrepo.New(conn).SetUserWorkosID(ctx, usersrepo.SetUserWorkosIDParams{
		WorkosID: conv.PtrToPGText(conv.PtrEmpty(workosUserID)),
		ID:       userID,
	})
	trequire.NoError(t, err)

	err = orgrepo.New(conn).AttachWorkOSUserToOrg(ctx, orgrepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     organizationID,
		UserID:             userID,
		WorkosMembershipID: conv.PtrToPGText(conv.PtrEmpty(workosMembershipID)),
	})
	trequire.NoError(t, err)
}
