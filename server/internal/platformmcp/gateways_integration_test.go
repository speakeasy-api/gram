package platformmcp

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type gatewayFixture struct {
	toolExposureFixture
	gateways *GatewayService
}

// seedGatewayFixture reuses the tool exposure fixture, whose hosted toolset
// server is exactly the kind of server a gateway refuses, and grants the
// mcp:write on the project that gateway creation checks.
func seedGatewayFixture(t *testing.T, name string) (context.Context, gatewayFixture) {
	t.Helper()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), name)
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, fixture.conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	engine := authz.NewEngine(logger, fixture.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	endpoints := mcpendpoints.NewService(logger, tracerProvider, fixture.conn, sessionManager, engine, audit.NewLogger(), nil, false)
	service, err := NewGatewayService(logger, fixture.conn, audit.NewLogger(), engine, NewLiveOrgAdminAuthorizer(fixture.conn, engine), admission.NewGuard(), endpoints, plugins.PublicationRequests{}, nil, testOperationBudget())
	require.NoError(t, err)
	return fixture.grantMCP(ctx, fixture.project.ID), gatewayFixture{toolExposureFixture: fixture, gateways: service}
}

// seedRemoteMember creates a live server in the project fronting its own
// remote upstream, which a gateway accepts.
func (f gatewayFixture) seedRemoteMember(t *testing.T, ctx context.Context, name string) uuid.UUID {
	t.Helper()
	remote := remotemcptest.SeedServer(t, ctx, f.conn, remotemcprepo.CreateServerParams{
		ProjectID: f.project.ID, TransportType: "streamable-http", Url: "https://upstream.example.test/mcp/" + uuid.NewString(),
	})
	server, err := mcpserversrepo.New(f.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: f.project.ID, Name: conv.ToPGText(name), Slug: conv.ToPGText(strings.ToLower(name) + "-" + uuid.NewString()[:4]),
		RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	return server.ID
}

func (f gatewayFixture) create(ctx context.Context, name string, members ...uuid.UUID) (GatewayOutput, error) {
	return f.gateways.CreateGateway(ctx, f.principal, CreateGatewayInput{ProjectID: f.project.ID.String(), Name: name, MCPIDs: uuidStrings(members)})
}

func (f gatewayFixture) set(ctx context.Context, gatewayID string, members ...uuid.UUID) (GatewayOutput, error) {
	return f.gateways.SetGatewayMembers(ctx, f.principal, SetGatewayMembersInput{ProjectID: f.project.ID.String(), GatewayID: gatewayID, MCPIDs: uuidStrings(members)})
}

func (f gatewayFixture) liveGatewayCount(t *testing.T, ctx context.Context) (gateways, addresses int) {
	t.Helper()
	rows, err := metamcprepo.New(f.conn).ListMetaMCPServers(ctx, metamcprepo.ListMetaMCPServersParams{OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID})
	require.NoError(t, err)
	endpoints, err := mcpendpointsrepo.New(f.conn).ListMCPEndpointsByProject(ctx, f.project.ID)
	require.NoError(t, err)
	for _, endpoint := range endpoints {
		if endpoint.MetaMcpServerID.Valid {
			addresses++
		}
	}
	return len(rows), addresses
}

func memberIDs(gateway *Gateway) []string {
	ids := make([]string, 0, len(gateway.Members))
	for _, member := range gateway.Members {
		ids = append(ids, member.MCPID)
	}
	return ids
}

// requireGatewayRefusal asserts the refusal code and returns the servers it
// names.
func requireGatewayRefusal(t *testing.T, err error, code string) []GatewayMemberRefusal {
	t.Helper()
	var refusal *GatewayError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, code, refusal.Code)
	return refusal.Members
}

func TestCreateGatewayCreatesGatewayAddressAndMembersTogether(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "platform_mcp_create_gateway")
	alpha, bravo := f.seedRemoteMember(t, ctx, "Alpha"), f.seedRemoteMember(t, ctx, "Bravo")

	gatewaysBefore, err := audittest.AuditLogCountByAction(ctx, f.conn, audit.ActionMetaMcpServerCreate)
	require.NoError(t, err)
	membersBefore, err := audittest.AuditLogCountByAction(ctx, f.conn, audit.ActionMetaMcpServerAddMember)
	require.NoError(t, err)

	out, err := f.create(ctx, "Connectors", bravo, alpha)
	require.NoError(t, err)
	require.Equal(t, gatewayOutcomeCreated, out.Outcome)
	require.Equal(t, "fresh_read_after_commit", out.SnapshotScope)
	require.Equal(t, "Connectors", out.Gateway.Name)
	require.Equal(t, "private", out.Gateway.Visibility)
	require.Equal(t, "public_only", out.Gateway.NetworkAccess)
	require.Equal(t, []string{bravo.String(), alpha.String()}, memberIDs(out.Gateway), "members keep the requested order")

	organizationSlug := projectOrganizationSlug(ctx, f.conn, f.principal.OrganizationID)
	require.Equal(t, []string{organizationSlug + "-connectors"}, out.Gateway.AddressSlugs, "the default address is the dashboard's")

	gatewaysAfter, err := audittest.AuditLogCountByAction(ctx, f.conn, audit.ActionMetaMcpServerCreate)
	require.NoError(t, err)
	membersAfter, err := audittest.AuditLogCountByAction(ctx, f.conn, audit.ActionMetaMcpServerAddMember)
	require.NoError(t, err)
	require.Equal(t, gatewaysBefore+1, gatewaysAfter)
	require.Equal(t, membersBefore+2, membersAfter)
}

func TestCreateGatewayFallsBackToAnIDAddressWhenTheNamedOneIsTaken(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "platform_mcp_create_gateway_address_taken")

	first, err := f.create(ctx, "Connectors")
	require.NoError(t, err)
	second, err := f.create(ctx, "Connectors")
	require.NoError(t, err)

	require.NotEqual(t, first.GatewayID, second.GatewayID, "a repeated call makes a second gateway")
	require.Len(t, second.Gateway.AddressSlugs, 1)
	require.NotEqual(t, first.Gateway.AddressSlugs[0], second.Gateway.AddressSlugs[0])
	require.True(t, strings.HasSuffix(second.Gateway.AddressSlugs[0], strings.ReplaceAll(second.GatewayID, "-", "")[24:]))
}

func TestCreateGatewayRefusesEveryServerThatCannotJoinAndCreatesNothing(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "platform_mcp_create_gateway_refused")
	alpha := f.seedRemoteMember(t, ctx, "Alpha")
	unknown := uuid.New()

	_, err := f.create(ctx, "Connectors", alpha, f.toolsetID, unknown)
	refused := requireGatewayRefusal(t, err, "members_refused")
	require.Len(t, refused, 2, "every server that cannot join is named, and only those")
	require.Equal(t, f.toolsetID.String(), refused[0].MCPID)
	require.Equal(t, "hosted_toolset_server", refused[0].Reason)
	require.Equal(t, unknown.String(), refused[1].MCPID)
	require.Equal(t, "not_in_project", refused[1].Reason)

	gateways, addresses := f.liveGatewayCount(t, ctx)
	require.Zero(t, gateways, "nothing was created")
	require.Zero(t, addresses, "not even the address")
}

func TestCreateGatewayRefusesTwoServersFrontingTheSameBackend(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "platform_mcp_create_gateway_shared_backend")
	first := f.seedRemoteMember(t, ctx, "First")
	server, err := mcpserversrepo.New(f.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: first, ProjectID: f.project.ID})
	require.NoError(t, err)
	twin, err := mcpserversrepo.New(f.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: f.project.ID, Name: conv.ToPGText("Twin"), Slug: conv.ToPGText("twin-" + uuid.NewString()[:4]),
		RemoteMcpServerID: server.RemoteMcpServerID, Visibility: "private",
	})
	require.NoError(t, err)

	_, err = f.create(ctx, "Connectors", first, twin.ID)
	refused := requireGatewayRefusal(t, err, "members_refused")
	require.Len(t, refused, 1)
	require.Equal(t, twin.ID.String(), refused[0].MCPID)
	require.Equal(t, "duplicate_backend", refused[0].Reason)
}

func TestCreateGatewayValidatesTheRequestBeforeWriting(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "platform_mcp_create_gateway_invalid")
	alpha := f.seedRemoteMember(t, ctx, "Alpha")

	_, err := f.create(ctx, "Connectors", alpha, alpha)
	requireGatewayRefusal(t, err, "invalid_request")
	_, err = f.create(ctx, "   ")
	requireGatewayRefusal(t, err, "invalid_request")
	_, err = f.gateways.CreateGateway(ctx, f.principal, CreateGatewayInput{ProjectID: f.project.ID.String(), Name: "Connectors", MCPIDs: []string{"alpha"}})
	requireGatewayRefusal(t, err, "invalid_request")

	gateways, _ := f.liveGatewayCount(t, ctx)
	require.Zero(t, gateways)
}

func TestCreateGatewayRequiresMCPWriteOnTheProject(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "platform_mcp_create_gateway_denied")
	// Only the fixture's own grants: mcp:write on one server, not the project.
	ctx = authz.GrantsToContext(ctx, f.grants)

	_, err := f.create(ctx, "Connectors")
	var denial *ExternalAuthorizationError
	require.ErrorAs(t, err, &denial)
	require.Equal(t, string(authz.ScopeMCPWrite), denial.RequiredScope)

	gateways, _ := f.liveGatewayCount(t, ctx)
	require.Zero(t, gateways)
}

func TestSetGatewayMembersAddsRemovesAndReordersInOneChange(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "platform_mcp_set_gateway_members")
	a, b, c := f.seedRemoteMember(t, ctx, "Alpha"), f.seedRemoteMember(t, ctx, "Bravo"), f.seedRemoteMember(t, ctx, "Charlie")
	created, err := f.create(ctx, "Connectors", a, b)
	require.NoError(t, err)
	gatewayID := uuid.MustParse(created.GatewayID)
	ctx = f.grantMCP(ctx, f.project.ID, gatewayID)

	removedBefore, err := audittest.AuditLogCountByAction(ctx, f.conn, audit.ActionMetaMcpServerRemoveMember)
	require.NoError(t, err)

	out, err := f.set(ctx, created.GatewayID, c, a)
	require.NoError(t, err)
	require.Equal(t, gatewayOutcomeUpdated, out.Outcome)
	require.Equal(t, []string{c.String()}, out.Added)
	require.Equal(t, []string{b.String()}, out.Removed)
	require.Equal(t, []string{c.String(), a.String()}, memberIDs(out.Gateway), "the stored order is the requested order")

	removedAfter, err := audittest.AuditLogCountByAction(ctx, f.conn, audit.ActionMetaMcpServerRemoveMember)
	require.NoError(t, err)
	require.Equal(t, removedBefore+1, removedAfter)

	again, err := f.set(ctx, created.GatewayID, c, a)
	require.NoError(t, err)
	require.Equal(t, gatewayOutcomeUnchanged, again.Outcome, "the same list again changes nothing")
	require.Equal(t, []string{c.String(), a.String()}, memberIDs(again.Gateway))

	emptied, err := f.set(ctx, created.GatewayID)
	require.NoError(t, err)
	require.Empty(t, emptied.Gateway.Members, "an empty list removes every member")
}

func TestSetGatewayMembersRefusalLeavesTheGatewayAsItWas(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "platform_mcp_set_gateway_members_refused")
	a, b := f.seedRemoteMember(t, ctx, "Alpha"), f.seedRemoteMember(t, ctx, "Bravo")
	created, err := f.create(ctx, "Connectors", a, b)
	require.NoError(t, err)
	ctx = f.grantMCP(ctx, f.project.ID, uuid.MustParse(created.GatewayID))

	// Dropping b would succeed on its own; the refused addition must undo it.
	_, err = f.set(ctx, created.GatewayID, a, f.toolsetID)
	refused := requireGatewayRefusal(t, err, "members_refused")
	require.Len(t, refused, 1)
	require.Equal(t, "hosted_toolset_server", refused[0].Reason)

	unchanged, err := f.set(ctx, created.GatewayID, a, b)
	require.NoError(t, err)
	require.Equal(t, gatewayOutcomeUnchanged, unchanged.Outcome, "the refused change left nothing behind")
}

func TestSetGatewayMembersRefusesAGatewayOutsideTheProject(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "platform_mcp_set_gateway_members_missing")
	missing := uuid.New()
	ctx = f.grantMCP(ctx, f.project.ID, missing)

	_, err := f.set(ctx, missing.String())
	requireGatewayRefusal(t, err, "not_found")
}
