package access

import (
	"context"
	"log"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// recordingEmailSender captures transactional sends so tests can assert on
// access-request notification emails without a live Loops client.
type recordingEmailSender struct {
	mu   sync.Mutex
	sent []loops.SendTransactionalInput
}

func (r *recordingEmailSender) SendTransactional(_ context.Context, input loops.SendTransactionalInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, input)
	return nil
}

func (r *recordingEmailSender) Sent() []loops.SendTransactionalInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]loops.SendTransactionalInput(nil), r.sent...)
}

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
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
	service     *Service
	conn        *pgxpool.Pool
	chConn      clickhouse.Conn
	roles       *MockRoleProvider
	emailSender *recordingEmailSender
	siteURL     *url.URL
}

func newTestAccessService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// workos_id is set by InitAuthContext via UpsertOrganizationMetadata (from the mock IDP's workos_id).

	roles := newMockRoleProvider(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	auditLogger := audit.NewLogger()

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	roleManager := NewRoleManager(logger, conn, roles, auditLogger)
	emailSender := &recordingEmailSender{mu: sync.Mutex{}, sent: nil}
	emailService := email.NewService(logger, emailSender, email.NewTemplateIDs(map[string]string{
		"access_request": "access-request-test-id",
	}), true)
	siteURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	svc := NewService(logger, tracerProvider, conn, chConn, sessionManager, roleManager, authzEngine, auditLogger, emailService, siteURL)

	return ctx, &testInstance{
		service:     svc,
		conn:        conn,
		chConn:      chConn,
		roles:       roles,
		emailSender: emailSender,
		siteURL:     siteURL,
	}
}

func seedOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     "Test Org",
		Slug:     "test-org",
		WorkosID: conv.PtrToPGText(nil),
	})
	require.NoError(t, err)
}

func seedGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope authz.Scope, resource string) {
	t.Helper()

	selectors, err := authz.NewSelector(scope, resource).MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func listPrincipalGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal) []accessrepo.ListPrincipalGrantsByOrgRow {
	t.Helper()

	grants, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal.String(),
	})
	require.NoError(t, err)

	return grants
}

func seedRole(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, role workos.Role) string {
	t.Helper()

	createdAt, err := time.Parse(time.RFC3339, role.CreatedAt)
	require.NoError(t, err)
	updatedAt, err := time.Parse(time.RFC3339, role.UpdatedAt)
	require.NoError(t, err)

	_, err = accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        role.Slug,
		WorkosName:        role.Name,
		WorkosDescription: conv.ToPGTextEmpty(role.Description),
		WorkosCreatedAt:   conv.ToPGTimestamptz(createdAt),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(updatedAt),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	row, err := accessrepo.New(conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: organizationID,
		WorkosSlug:     role.Slug,
	})
	require.NoError(t, err)

	return row.ID.String()
}

func seededRolePrincipal(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, roleSlug string) urn.Principal {
	t.Helper()

	row, err := accessrepo.New(conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: organizationID,
		WorkosSlug:     roleSlug,
	})
	require.NoError(t, err)
	return urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+row.ID.String())
}

func seedGlobalRole(t *testing.T, ctx context.Context, conn *pgxpool.Pool, role workos.Role) string {
	t.Helper()

	createdAt, err := time.Parse(time.RFC3339, role.CreatedAt)
	require.NoError(t, err)
	updatedAt, err := time.Parse(time.RFC3339, role.UpdatedAt)
	require.NoError(t, err)

	err = accessrepo.New(conn).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
		WorkosSlug:        role.Slug,
		WorkosName:        role.Name,
		WorkosDescription: conv.ToPGTextEmpty(role.Description),
		WorkosCreatedAt:   conv.ToPGTimestamptz(createdAt),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(updatedAt),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	row, err := accessrepo.New(conn).GetGlobalRoleBySlug(ctx, role.Slug)
	require.NoError(t, err)

	return row.ID.String()
}

func seedRoleAssignment(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, userID string, member workos.Member) {
	t.Helper()

	updatedAt := time.Now().UTC()
	if member.UpdatedAt != "" {
		parsed, err := time.Parse(time.RFC3339, member.UpdatedAt)
		require.NoError(t, err)
		updatedAt = parsed
	} else if member.CreatedAt != "" {
		parsed, err := time.Parse(time.RFC3339, member.CreatedAt)
		require.NoError(t, err)
		updatedAt = parsed
	}

	for _, slug := range member.RoleSlugs {
		inserted, err := accessrepo.New(conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
			OrganizationID:     organizationID,
			WorkosUserID:       member.UserID,
			WorkosRoleSlug:     slug,
			UserID:             conv.ToPGTextEmpty(userID),
			WorkosMembershipID: conv.ToPGTextEmpty(member.ID),
			WorkosUpdatedAt:    conv.ToPGTimestamptz(updatedAt),
			WorkosLastEventID:  conv.ToPGTextEmpty(""),
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), inserted)
	}
}

// seedDisconnectedUser creates a user in the users table with a workos_id but
// does NOT insert into organization_user_relationships, simulating a WorkOS
// user who hasn't been connected to the Gram org.
func seedDisconnectedUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, userID string, email string, displayName string, workosUserID string) {
	t.Helper()

	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: displayName,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	err = usersrepo.New(conn).OverwriteUserWorkosID(ctx, usersrepo.OverwriteUserWorkosIDParams{
		WorkosID: conv.ToPGText(workosUserID),
		ID:       userID,
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

	err = usersrepo.New(conn).OverwriteUserWorkosID(ctx, usersrepo.OverwriteUserWorkosIDParams{
		WorkosID: conv.ToPGText(workosUserID),
		ID:       userID,
	})
	require.NoError(t, err)

	err = orgrepo.New(conn).AttachWorkOSUserToOrg(ctx, orgrepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.PtrToPGText(conv.PtrEmpty(workosMembershipID)),
	})
	require.NoError(t, err)
}
