package remotemcp_test

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
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
	service         *remotemcp.Service
	conn            *pgxpool.Pool
	enc             *encryption.Client
	sessionManager  *sessions.Manager
	shadowMCPClient *shadowmcp.Client
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	// servicePolicy blocks loopback / private ranges so validateURL exercises
	// the real production CIDR set, and uses a mock resolver so hostname-based
	// test cases are deterministic.
	servicePolicy := guardian.NewDefaultPolicy(
		testenv.NewTracerProvider(t),
		guardian.WithResolver(newRemoteMCPMockResolver()),
	)
	return newTestServiceWithPolicy(t, servicePolicy)
}

// newTestServiceWithPolicy is the variant of [newTestService] that lets the
// caller override the guardian.Policy. Discovery tests use this with an
// unsafe policy so the service can dial httptest.NewServer on 127.0.0.1.
func newTestServiceWithPolicy(t *testing.T, servicePolicy *guardian.Policy) (context.Context, *testInstance) {
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

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	enc := testenv.NewEncryptionClient(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	auditLogger := audit.NewLogger()

	svc := remotemcp.NewService(logger, tracerProvider, conn, sessionManager, enc, authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), servicePolicy, auditLogger)

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	accessStore := accesscontrol.NewRedisStore(cacheAdapter, accesscontrol.AlphaTTL)
	shadowMCPClient := shadowmcp.NewClient(logger, conn, cacheAdapter, accessStore)

	return ctx, &testInstance{
		service:         svc,
		conn:            conn,
		enc:             enc,
		sessionManager:  sessionManager,
		shadowMCPClient: shadowMCPClient,
	}
}

// requireStoredSecretValue asserts that a secret header's stored ciphertext
// decrypts back to want. It reads through the same unredacted path the MCP
// proxy uses, which is the only consumer of real header values. A
// double-encrypted value fails here rather than silently reaching an upstream
// server as garbage.
func requireStoredSecretValue(t *testing.T, ctx context.Context, ti *testInstance, serverID string, name string, want string) {
	t.Helper()

	parsed, err := uuid.Parse(serverID)
	require.NoError(t, err)

	headers, err := remotemcp.NewHeaders(testenv.NewLogger(t), ti.conn, ti.enc).ListHeaders(ctx, parsed, false)
	require.NoError(t, err)

	for _, header := range headers {
		if header.Name != name {
			continue
		}

		require.True(t, header.Value.Valid, "header %q has no stored value", name)
		require.Equal(t, want, header.Value.String)
		return
	}

	t.Fatalf("header %q not found on server %s", name, serverID)
}

// requireStoredEnvSourcedValue asserts the empty-non-null sentinel that marks
// an environment-sourced header (ADR-0002).
func requireStoredEnvSourcedValue(t *testing.T, ctx context.Context, ti *testInstance, serverID string, name string) {
	t.Helper()

	parsed, err := uuid.Parse(serverID)
	require.NoError(t, err)

	headers, err := remotemcp.NewHeaders(testenv.NewLogger(t), ti.conn, ti.enc).ListHeaders(ctx, parsed, false)
	require.NoError(t, err)

	for _, header := range headers {
		if header.Name != name {
			continue
		}

		require.True(t, header.Value.Valid, "header %q value must be non-null empty", name)
		require.Empty(t, header.Value.String)
		require.False(t, header.ValueFromRequestHeader.Valid)
		require.False(t, header.IsSecret)
		return
	}

	t.Fatalf("header %q not found on server %s", name, serverID)
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

// createTestServer creates a remote MCP server in the auth context's project.
func createTestServer(t *testing.T, ctx context.Context, ti *testInstance) *types.RemoteMcpServer {
	t.Helper()

	server, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)

	return server
}

// newCreateServerHeaderPayload builds a create-header payload with every field
// zeroed, then applies opts. Tests set only the fields they care about, which
// keeps the exhaustruct-mandated zero fields out of every call site.
func newCreateServerHeaderPayload(serverID string, name string, opts func(*gen.CreateServerHeaderPayload)) *gen.CreateServerHeaderPayload {
	payload := &gen.CreateServerHeaderPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		RemoteMcpServerID:      serverID,
		Name:                   name,
		Description:            nil,
		IsRequired:             nil,
		IsSecret:               nil,
		Value:                  nil,
		ValueFromRequestHeader: nil,
	}

	if opts != nil {
		opts(payload)
	}

	return payload
}

// newUpdateServerHeaderPayload mirrors [newCreateServerHeaderPayload] for updates.
func newUpdateServerHeaderPayload(headerID string, name string, opts func(*gen.UpdateServerHeaderPayload)) *gen.UpdateServerHeaderPayload {
	payload := &gen.UpdateServerHeaderPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		ID:                     headerID,
		Name:                   name,
		Description:            nil,
		IsRequired:             nil,
		IsSecret:               nil,
		Value:                  nil,
		ValueFromRequestHeader: nil,
	}

	if opts != nil {
		opts(payload)
	}

	return payload
}

// seedOtherProjectServer creates a second project in the same organization and
// seeds a remote MCP server into it. Used to prove the header endpoints refuse
// to address rows outside the caller's project.
func seedOtherProjectServer(t *testing.T, ctx context.Context, ti *testInstance) repo.RemoteMcpServer {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Other Project",
		Slug:           "proj-" + uuid.NewString()[:8],
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	return remotemcptest.SeedServer(t, ctx, ti.conn, repo.CreateServerParams{
		ID:            uuid.Nil,
		ProjectID:     otherProject.ID,
		Name:          pgtype.Text{String: "", Valid: false},
		Slug:          conv.ToPGText("other-project-server-" + uuid.NewString()[:8]),
		TransportType: "streamable-http",
		Url:           "https://other.example.com",
	})
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
