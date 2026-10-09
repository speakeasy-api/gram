package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisserver "github.com/alicebob/miniredis/v2/server"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/tunnel/route"
)

// blockingTunnelConnections holds every read until its context ends, the way
// a degraded runtime store would.
type blockingTunnelConnections struct {
	sawDeadline chan bool
}

func (b *blockingTunnelConnections) Connections(ctx context.Context, _ string) ([]route.Connection, error) {
	_, hasDeadline := ctx.Deadline()
	b.sawDeadline <- hasDeadline
	<-ctx.Done()
	return nil, fmt.Errorf("read tunnel connections: %w", ctx.Err())
}

// tunnelBoundaryTargets are tunneled MCP servers outside the caller's project,
// stored in the caller's own database so a lookup could reach them.
type tunnelBoundaryTargets struct {
	siblingProject uuid.UUID
	siblingMCP     uuid.UUID
	foreignProject uuid.UUID
	foreignMCP     uuid.UUID
}

func seedTunnelBoundaryTargets(t *testing.T, fixture tunnelStatusFixture) tunnelBoundaryTargets {
	t.Helper()
	sibling, err := projectsrepo.New(fixture.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Sibling project", Slug: "sibling-" + uuid.NewString()[:8], OrganizationID: fixture.principal.OrganizationID,
	})
	require.NoError(t, err)
	_, siblingMCP := seedTunneledMCP(t, fixture.conn, sibling.ID, "sibling-inventory", "private")
	_, foreignProject := seedRegistrationLifecycle(t, t.Context(), fixture.conn)
	_, foreignMCP := seedTunneledMCP(t, fixture.conn, foreignProject.ID, "foreign-inventory", "private")
	return tunnelBoundaryTargets{siblingProject: sibling.ID, siblingMCP: siblingMCP, foreignProject: foreignProject.ID, foreignMCP: foreignMCP}
}

func TestTunneledBoundariesHideOtherProjectsAndOrganizations(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_boundaries")
	fixture.grantSetupAdmin(t)
	targets := seedTunnelBoundaryTargets(t, fixture)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	for _, input := range []GetTunneledMCPSetupHandoffInput{
		{ProjectID: fixture.project.ID.String(), MCPID: targets.siblingMCP.String()},
		{ProjectID: fixture.project.ID.String(), MCPID: targets.foreignMCP.String()},
		{ProjectID: targets.siblingProject.String()},
		{ProjectID: targets.siblingProject.String(), MCPID: targets.siblingMCP.String()},
		{ProjectID: targets.foreignProject.String()},
		{ProjectID: targets.foreignProject.String(), MCPID: targets.foreignMCP.String()},
	} {
		_, err := harness.invoke(t, ctx, input)
		requireTunneledSetupRefusal(t, err, "not_found")
	}
	require.Len(t, harness.limiter.keys, 4, "the two in-project targets passed authorization and were metered; the hidden projects were not")

	connections := &recordingTunnelConnections{}
	reader, readerCtx := fixture.reader(t, connections)
	for _, target := range []struct{ project, mcp uuid.UUID }{
		{fixture.project.ID, targets.siblingMCP},
		{fixture.project.ID, targets.foreignMCP},
		{targets.siblingProject, targets.siblingMCP},
		{targets.foreignProject, targets.foreignMCP},
	} {
		require.Nil(t, reader.tunnelStatus.Status(readerCtx, fixture.principal, target.project, target.mcp))
	}
	require.Zero(t, connections.calls.Load(), "no runtime read for a server outside the caller's readable project")
}

func TestTunneledSetupHandoffHidesDeletedServerAndProject(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_deleted")
	fixture.grantSetupAdmin(t)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	_, err := mcpserversrepo.New(fixture.conn).DeleteMCPServer(t.Context(), mcpserversrepo.DeleteMCPServerParams{ID: fixture.wrapperID, ProjectID: fixture.project.ID})
	require.NoError(t, err)
	_, err = harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String(), MCPID: fixture.wrapperID.String()})
	requireTunneledSetupRefusal(t, err, "not_found")

	_, err = projectsrepo.New(fixture.conn).DeleteProject(t.Context(), fixture.project.ID)
	require.NoError(t, err)
	_, err = harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String()})
	requireTunneledSetupRefusal(t, err, "not_found")
}

func TestTunneledSetupHandoffHonoursBlockedGrants(t *testing.T) {
	t.Parallel()

	for _, blocked := range []struct {
		name     string
		database string
		scope    authz.Scope
		resource func(tunnelStatusFixture) string
		dims     bool
		input    func(tunnelStatusFixture) GetTunneledMCPSetupHandoffInput
	}{
		{
			name: "blocked project", database: "platform_mcp_tunneled_setup_blocked_project", scope: authz.ScopeProjectBlockedRead, dims: false,
			resource: func(f tunnelStatusFixture) string { return f.project.ID.String() },
			input: func(f tunnelStatusFixture) GetTunneledMCPSetupHandoffInput {
				return GetTunneledMCPSetupHandoffInput{ProjectID: f.project.ID.String()}
			},
		},
		{
			name: "blocked server", database: "platform_mcp_tunneled_setup_blocked_server", scope: authz.ScopeMCPBlockedRead, dims: true,
			resource: func(f tunnelStatusFixture) string { return f.wrapperID.String() },
			input: func(f tunnelStatusFixture) GetTunneledMCPSetupHandoffInput {
				return GetTunneledMCPSetupHandoffInput{ProjectID: f.project.ID.String(), MCPID: f.wrapperID.String()}
			},
		},
	} {
		t.Run(blocked.name, func(t *testing.T) {
			t.Parallel()
			fixture := seedTunnelStatusFixture(t, blocked.database)
			fixture.grantSetupAdmin(t)
			fixture.grant(t, blocked.scope, blocked.resource(fixture), blocked.dims)
			harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

			_, err := harness.invoke(t, ctx, blocked.input(fixture))
			requireTunneledSetupRefusal(t, err, "not_found")
			require.Empty(t, harness.limiter.keys)
		})
	}
}

func TestTunneledSetupHandoffRejectsExplicitNilServer(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_nil_server")
	fixture.grantSetupAdmin(t)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	_, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String(), MCPID: uuid.Nil.String()})
	requireTunneledSetupRefusal(t, err, "invalid_request")
	require.Empty(t, harness.limiter.keys, "a supplied server never falls back to the add form")
}

func TestTunnelStatusReportsUnknownWhenSourceReadFails(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_source_failure")
	fixture.grantProjectSourceRead(t)
	_, ctx := fixture.reader(t, nil)
	engine := authz.NewEngine(testenv.NewLogger(t), fixture.conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())

	closed, err := pgxpool.New(t.Context(), fixture.conn.Config().ConnString())
	require.NoError(t, err)
	closed.Close()
	connections := &recordingTunnelConnections{}
	service := &TunnelStatusService{logger: testenv.NewLogger(t), db: closed, authz: engine, connections: connections}

	require.Equal(t, &MCPTunnel{ConnectionStatus: TunnelConnectionUnknown}, service.Status(ctx, fixture.principal, fixture.project.ID, fixture.wrapperID))
	require.Zero(t, connections.calls.Load())
}

func TestTunnelStatusBoundsRuntimeRead(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_runtime_deadline")
	fixture.grantProjectSourceRead(t)
	connections := &blockingTunnelConnections{sawDeadline: make(chan bool, 1)}
	reader, ctx := fixture.reader(t, connections)

	started := time.Now()
	mcp := fixture.getMCP(t, reader, ctx, fixture.wrapperID)

	require.Equal(t, &MCPTunnel{ConnectionStatus: TunnelConnectionUnknown}, mcp.Tunnel)
	require.True(t, <-connections.sawDeadline, "the runtime read runs under the enrichment deadline")
	require.Less(t, time.Since(started), tunnelStatusReadTimeout+2*time.Second)
}

func TestTunnelStatusIsIndependentOfDisabledServer(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_disabled")
	fixture.grantProjectSourceRead(t)
	_, disabledID := seedTunneledMCP(t, fixture.conn, fixture.project.ID, "disabled-inventory", "disabled")
	reader, ctx := fixture.reader(t, &recordingTunnelConnections{connections: []route.Connection{{GatewaySessionID: "session"}}})

	mcp := fixture.getMCP(t, reader, ctx, disabledID)

	require.False(t, mcp.EffectiveEnabled)
	require.Equal(t, "unsupported", mcp.Readiness.State)
	require.Equal(t, &MCPTunnel{ConnectionStatus: TunnelConnectionConnected}, mcp.Tunnel, "a connected agent says nothing about whether the server is enabled or ready")
	encoded, err := json.Marshal(mcp)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), tunnelStatusSentinel)
}

// The production runtime store is go-redis without ContextTimeoutEnabled, so a
// socket read ignores the caller's deadline. A Redis reply slower than the
// remaining budget must still yield unknown, at the deadline rather than when
// the reply finally arrives.
func TestTunnelStatusEnforcesDeadlineOnRedisRuntimeStore(t *testing.T) {
	t.Parallel()

	const (
		// redisReplyDelay is longer than the caller's remaining budget and
		// shorter than the client's read timeout, so only the enrichment
		// deadline can cut the read short.
		redisReplyDelay = 250 * time.Millisecond
		callerBudget    = 100 * time.Millisecond
	)

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_redis_deadline")
	fixture.grantProjectSourceRead(t)
	server := miniredis.RunT(t)
	// The options newRedisClient uses in production, ContextTimeoutEnabled
	// left off.
	client := redis.NewClient(&redis.Options{
		Addr:            server.Addr(),
		DialTimeout:     time.Second,
		ReadTimeout:     300 * time.Millisecond,
		WriteTimeout:    time.Second,
		DisableIdentity: true,
	})
	t.Cleanup(func() { _ = client.Close() })
	store := route.NewRedis(client)
	require.NoError(t, store.PublishConnections(t.Context(), fixture.tunnelID.String(), "gateway", []route.Connection{{GatewaySessionID: "session"}}, time.Minute))

	reader, ctx := fixture.reader(t, store)
	connected := fixture.getMCP(t, reader, ctx, fixture.wrapperID)
	require.Equal(t, &MCPTunnel{ConnectionStatus: TunnelConnectionConnected}, connected.Tunnel, "the real store reports the published agent")

	delayed := make(chan struct{}, 1)
	server.Server().SetPreHook(func(_ *redisserver.Peer, cmd string, _ ...string) bool {
		if strings.EqualFold(cmd, "HGETALL") {
			delayed <- struct{}{}
			<-time.NewTimer(redisReplyDelay).C
		}
		return false
	})
	bounded, cancel := context.WithTimeout(ctx, callerBudget)
	defer cancel()
	started := time.Now()

	status := reader.tunnelStatus.Status(bounded, fixture.principal, fixture.project.ID, fixture.wrapperID)

	select {
	case <-delayed:
	default:
		require.Fail(t, "the read must reach the delayed Redis reply, not stop at authorization or the source lookup")
	}
	require.Equal(t, &MCPTunnel{ConnectionStatus: TunnelConnectionUnknown}, status, "a reply after the deadline is never classified")
	require.Less(t, time.Since(started), redisReplyDelay, "the read is abandoned at the deadline, not when Redis replies")
}
