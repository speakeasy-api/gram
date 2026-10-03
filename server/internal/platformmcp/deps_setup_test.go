package platformmcp

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/mcpapprovaltest"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/tunnel/route"
)

// newTestTunnelClient routes tunneled MCP traffic through the Redis route
// table the way the server does.
func newTestTunnelClient(t *testing.T, policy *guardian.Policy) *tunnelrouting.HTTPClient {
	t.Helper()
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return tunnelrouting.NewHTTPClient(route.NewRedis(redisClient), "test-forward-token", policy, nil)
}

// newTestManagementService builds the dashboard management service over conn
// the way the server wires it.
func newTestManagementService(t *testing.T, conn *pgxpool.Pool) *ManagementService {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessions := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("platform-mcp-management-"+uuid.NewString()), billing.NewStubClient(logger, tracerProvider))
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	gate := NewOrganizationGate(testCapabilityChecker{enabled: true, err: nil})
	registrationGate := NewCatalogRegistrationGate(gate)
	store := NewRegistrationStore(conn)
	catalog := NewRegistryCatalogSources(nil)
	readiness := NewReadinessService(store, registrationGate, NewProviderAdapters(nil), allowOperationLimiter{}, allowBudget())
	distributions := testDistributionService(t, conn, func(context.Context, uuid.UUID, string, string) error { return nil })
	return NewManagementService(logger, tracerProvider, conn, sessions, engine, gate, NewLiveOrgAdminAuthorizer(conn, engine), "https://gram.example.test/platform-mcp", NewRegistrationService(catalog, registrationGate, store), readiness, distributions, testServicesKey, catalog)
}

// newTestApprovals builds the real MCP approval service over conn the way the
// server wires it.
func newTestApprovals(t *testing.T, conn *pgxpool.Pool, flags feature.Provider) *mcpapproval.Service {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	chConn, err := platformMCPInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	sessions := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("platform-mcp-approvals-"+uuid.NewString()), billing.NewStubClient(logger, tracerProvider))
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	return mcpapproval.NewService(logger, tracerProvider, conn, sessions, engine, flags, audit.NewLogger(), mcpapprovaltest.NewAssembler(telemetryrepo.New(chConn)), mcpapprovaltest.StartResearch)
}

// newTestRiskPolicyCore builds the risk policy mutation core with the real
// approval intake and the real shadow MCP cache invalidator.
func newTestRiskPolicyCore(t *testing.T, conn *pgxpool.Pool, flags feature.Provider) *policycore.MutationCore {
	t.Helper()

	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	serverURL, err := url.Parse("https://gram.example.test")
	require.NoError(t, err)
	shadowMCP := shadowmcp.NewClient(testenv.NewLogger(t), conn, cache.NewRedisCacheAdapter(redisClient), serverURL)
	return risk.NewPolicyMutationCore(conn, audit.NewLogger(), newTestApprovals(t, conn, flags), noopRiskPolicySignaler{}, shadowMCP)
}
