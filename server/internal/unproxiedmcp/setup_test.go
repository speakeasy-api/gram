package unproxiedmcp_test

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/unproxiedmcp"
)

// blockedTestHost resolves to a private IP via the test mock resolver, so it
// is rejected by the test guardian.Policy at validation time.
const blockedTestHost = "internal.test"

// unresolvableTestHost returns a resolver error via the test mock resolver,
// so it is rejected by the test guardian.Policy at validation time.
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
	service *unproxiedmcp.Service
	conn    *pgxpool.Pool
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
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

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	auditLogger := audit.NewLogger()

	// servicePolicy blocks loopback / private ranges so validateServerURL
	// exercises the real production CIDR set, and uses a mock resolver so
	// hostname-based test cases are deterministic.
	servicePolicy := guardian.NewDefaultPolicy(
		tracerProvider,
		guardian.WithResolver(newUnproxiedMCPMockResolver()),
	)

	svc := unproxiedmcp.NewService(logger, tracerProvider, conn, sessionManager, authz.NewEngine(logger, conn, chConn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), servicePolicy, auditLogger)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
	}
}

// withStaffEmail overrides the auth context's email to a Speakeasy-owned
// domain so CreateServer's staff gate passes.
func withStaffEmail(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	email := "staffer@speakeasyapi.dev"
	authCtx.Email = &email

	return contextvalues.SetAuthContext(ctx, authCtx)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// newUnproxiedMCPMockResolver returns a [dns.Resolver] used to make hostname
// validation deterministic in tests. blockedTestHost resolves to a private IP
// (which the test guardian.Policy blocks), unresolvableTestHost returns a
// resolver error, and any other hostname resolves to a public IP.
func newUnproxiedMCPMockResolver() dns.Resolver {
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
