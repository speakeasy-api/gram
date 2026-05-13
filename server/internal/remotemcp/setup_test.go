package remotemcp_test

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// blockedTestHost resolves to a private IP via the test mock resolver, so it
// is rejected by the test guardian.Policy at validation time.
const blockedTestHost = "internal.test"

// unresolvableTestHost returns a resolver error via the test mock resolver, so
// it is rejected by the test guardian.Policy at validation time.
const unresolvableTestHost = "broken.test"

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *remotemcp.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	// Two guardian policies are required because the test setup has two
	// conflicting needs:
	//
	//   - sessionsPolicy permits loopback so the session manager can dial the
	//     in-process mock IDP httptest.Server (which listens on 127.0.0.1).
	//
	//   - servicePolicy blocks loopback / private ranges so validateURL
	//     exercises the real production CIDR set, and uses a mock resolver so
	//     hostname-based test cases are deterministic.
	sessionsPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	servicePolicy := guardian.NewDefaultPolicy(
		tracerProvider,
		guardian.WithResolver(newRemoteMCPMockResolver()),
	)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, sessionsPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	enc := testenv.NewEncryptionClient(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	auditLogger := audit.NewLogger()

	svc := remotemcp.NewService(logger, tracerProvider, conn, sessionManager, enc, authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache), servicePolicy, auditLogger)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

func withExactAccessGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "remotemcp-rbac-grants-"+uuid.NewString())
	for _, grant := range grants {
		selectors, _ := grant.Selector.MarshalJSON()
		_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   principal,
			Scope:          string(grant.Scope),
			Selectors:      selectors,
		})
		require.NoError(t, err)
	}

	loadedGrants, err := authz.LoadGrants(ctx, conn, authCtx.ActiveOrganizationID, []urn.Principal{principal})
	require.NoError(t, err)

	return authz.GrantsToContext(ctx, loadedGrants)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// newRemoteMCPMockResolver returns a [dns.Resolver] used to make hostname
// validation deterministic in tests. blockedTestHost resolves to a private IP
// (which the test guardian.Policy blocks), unresolvableTestHost returns a
// resolver error, and any other hostname resolves to a public IP.
func newRemoteMCPMockResolver() dns.Resolver {
	return dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(ctx context.Context, network, host string) ([]net.IP, error) {
			switch host {
			case blockedTestHost:
				return []net.IP{net.ParseIP("10.0.0.1")}, nil
			case unresolvableTestHost:
				return nil, errors.New("mock resolver: nxdomain")
			default:
				return []net.IP{net.ParseIP("1.2.3.4")}, nil
			}
		},
	})
}
