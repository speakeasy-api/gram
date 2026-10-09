package workloadpolicy_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	agentsRepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy"
)

// A Claude Tag trust policy, which is the shape this API was reduced to serve:
// the organization is fixed, and the trailing agent id is minted per Slack
// channel and never shown in Anthropic's console, so it cannot be admitted
// exactly in advance.
const (
	anthropicIssuer = "https://identity.anthropic.com"
	anthropicJWKS   = "https://identity.anthropic.com/.well-known/jwks.json"
	fleetStem       = "wimse://identity.anthropic.com/org/org-abc/agent/"
	fleetRule       = fleetStem + "*"
	channelOne      = fleetStem + "agent-111"
)

// testServerURL is the deployment's server URL, where shared authorization
// servers are served.
const testServerURL = "https://gram.example.com"

// testAuthenticationHostURL is the deployment's authentication host, where
// shared authorization servers of issuers pinned to or opted in to it are
// served.
const testAuthenticationHostURL = "https://id.example.com"

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type testInstance struct {
	service   *workloadpolicy.Service
	conn      *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
	userID    string
}

// newTestService builds the service against a cloned database and a dashboard
// session context with admin grants. The context also carries a project, which
// a session never selects in production; resolve must ignore it.
func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("workloadpolicy-"+uuid.NewString()), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	service := workloadpolicy.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger(), authserver.Hosts{ServerURL: testServerURL, AuthenticationHostBaseURL: testAuthenticationHostURL, PlatformHosts: nil})

	return ctx, &testInstance{
		service:   service,
		conn:      conn,
		orgID:     authCtx.ActiveOrganizationID,
		projectID: *authCtx.ProjectID,
		userID:    authCtx.UserID,
	}
}

// newAgent creates an agent in the caller's organization. Admission assigns one,
// and a subject admitted without an agent is a state this API refuses to create.
func newAgent(t *testing.T, ctx context.Context, ti *testInstance, name string) uuid.UUID {
	t.Helper()

	agent, err := agentsRepo.New(ti.conn).CreateAgent(ctx, agentsRepo.CreateAgentParams{
		OrganizationID: ti.orgID,
		OwnerUserID:    ti.userID,
		ProjectID:      conv.ToNullUUID(ti.projectID),
		Name:           name,
	})
	require.NoError(t, err)

	return agent.ID
}

// withScopes replaces the caller's grants with exactly the ones named, so a
// test can assert what a workload:read holder can and cannot do.
func withScopes(t *testing.T, ctx context.Context, ti *testInstance, scopes ...authz.Scope) context.Context {
	t.Helper()

	grants := make([]authz.Grant, 0, len(scopes))
	for _, scope := range scopes {
		grants = append(grants, authz.NewGrant(scope, ti.orgID))
	}

	return authztest.WithExactGrants(t, ctx, grants...)
}

// withoutProject returns ctx with the caller's project cleared, as a dashboard
// session arrives: the service is organization-wide, so a session names no
// project and only an API key selects one.
func withoutProject(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgOnly := *authCtx
	orgOnly.ProjectID = nil
	orgOnly.ProjectSlug = nil

	return contextvalues.SetAuthContext(ctx, &orgOnly)
}

// asAPIKey returns ctx as a legacy API key arrives: the key names the project
// already on the context, and only a key reaches the project tier. A legacy key
// skips RBAC enforcement, so scope tests stay on the session context.
func asAPIKey(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	keyed := *authCtx
	keyed.APIKeyID = uuid.NewString()

	return contextvalues.WithLegacyAPIKeyAuthorization(ctx, &keyed)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareErr *oops.ShareableError
	require.ErrorAs(t, err, &shareErr)
	require.Equal(t, code, shareErr.Code, "error: %v", err)
}
