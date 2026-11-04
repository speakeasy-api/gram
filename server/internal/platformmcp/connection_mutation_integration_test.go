package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type connectionMutationFixture struct {
	gatewayFixture
	orgSlug  string
	hostedID uuid.UUID
	// hostedEndpoint is the hosted server's platform endpoint.
	hostedEndpoint uuid.UUID
	mutations      *MCPConnectionMutationService
}

func seedConnectionMutationFixture(t *testing.T, name string, eligibility networkaccess.EligibilityChecker) (context.Context, connectionMutationFixture) {
	t.Helper()
	ctx, gateway := seedGatewayFixture(t, name)
	orgSlug := projectOrganizationSlug(ctx, gateway.conn, gateway.principal.OrganizationID)
	// testenv.SetHostedMCP may already have given the hosted server this endpoint.
	endpoint, err := mcpendpointsrepo.New(gateway.conn).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug: orgSlug + "-hosted", CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		endpoint, err = mcpendpointsrepo.New(gateway.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
			ProjectID: gateway.project.ID, McpServerID: uuid.NullUUID{UUID: gateway.toolsetID, Valid: true}, Slug: orgSlug + "-hosted",
		})
	}
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: gateway.toolsetID, Valid: true}, endpoint.McpServerID)
	// A legacy toolset whose alias follows its endpoint.
	testenv.SetLegacyAlias(t, ctx, gateway.conn, gateway.project.ID, gateway.toolsetID, endpoint.Slug, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, gateway.conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	engine := authz.NewEngine(logger, gateway.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	writer := mcpendpoints.NewService(logger, tracerProvider, gateway.conn, sessionManager, engine, audit.NewLogger(), nil, false)
	mutations, err := NewMCPConnectionMutationService(gateway.conn, NewMCPConnectionSettingsService(gateway.conn), writer, audit.NewLogger(), engine, eligibility, plugins.PublicationRequests{}, nil)
	require.NoError(t, err)
	return ctx, connectionMutationFixture{gatewayFixture: gateway, orgSlug: orgSlug, hostedID: gateway.toolsetID, hostedEndpoint: endpoint.ID, mutations: mutations}
}

func (f connectionMutationFixture) settings(t *testing.T, ctx context.Context, kind MCPConnectionSettingsTargetKind, target uuid.UUID) MCPConnectionSettings {
	t.Helper()
	settings, err := NewMCPConnectionSettingsService(f.conn).Get(ctx, f.principal, GetMCPConnectionSettingsInput{ProjectID: f.project.ID.String(), TargetKind: kind, TargetID: target.String()})
	require.NoError(t, err)
	return settings
}

func (f connectionMutationFixture) setAddress(ctx context.Context, kind MCPConnectionSettingsTargetKind, target uuid.UUID, version, endpointID, slug, domainID string, confirmed bool) (MCPConnectionMutationOutput, error) {
	return f.mutations.SetAddress(ctx, f.principal, SetMCPAddressInput{
		ProjectID: f.project.ID.String(), TargetKind: kind, TargetID: target.String(), EndpointID: endpointID, Slug: slug,
		CustomDomainID: domainID, ExpectedVersion: version, IdempotencyKey: uuid.NewString(), Confirmed: confirmed,
	})
}

func (f connectionMutationFixture) seedDomain(t *testing.T, ctx context.Context, organizationID string, verified, activated bool) uuid.UUID {
	t.Helper()
	queries := customdomainsrepo.New(f.conn)
	domain, err := queries.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: organizationID, Domain: "mcp-" + uuid.NewString()[:8] + ".example.test", ProvisionerKind: "local", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	_, err = queries.UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{Verified: verified, Activated: activated, ProvisionerKind: "local", ID: domain.ID})
	require.NoError(t, err)
	return domain.ID
}

func (f connectionMutationFixture) seedIngress(t *testing.T, ctx context.Context, status string) {
	t.Helper()
	fixtures := testrepo.New(f.conn)
	require.NoError(t, fixtures.InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: uuid.New(), OrganizationID: f.principal.OrganizationID, DnsName: pgtype.Text{String: "private.example.ts.net", Valid: true},
	}))
	require.NoError(t, fixtures.SetNetworkIngressObservationFixture(ctx, testrepo.SetNetworkIngressObservationFixtureParams{OrganizationID: f.principal.OrganizationID, Status: status}))
}

// requireConnectionRefusal asserts the refusal an agent reads: its code and a part of its message.
func requireConnectionRefusal(t *testing.T, err error, code, message string) map[string]any {
	t.Helper()
	result, ok := mcpConnectionMutationToolResult(err)
	require.True(t, ok, "expected a readable refusal, got %v", err)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(text.Text), &payload))
	require.Equal(t, code, payload["code"], text.Text)
	require.Contains(t, payload["message"], message)
	return payload
}

// stale returns a well-formed version that differs from version.
func stale(version string) string {
	if version[0] == '0' {
		return "1" + version[1:]
	}
	return "0" + version[1:]
}

func countAudit(t *testing.T, ctx context.Context, f connectionMutationFixture, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(ctx, f.conn, action)
	require.NoError(t, err)
	return count
}

func TestSetMCPAddressPreviewWritesNothing(t *testing.T) {
	t.Parallel()
	ctx, f := seedConnectionMutationFixture(t, "platform_mcp_set_address_preview", allowConnectionNetworkAccess{})
	before := f.settings(t, ctx, MCPConnectionSettingsMCPServer, f.hostedID)
	audits := countAudit(t, ctx, f, audit.ActionMcpEndpointCreate)

	_, err := f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, "", f.orgSlug+"-second", "", false)
	var preview *connectionMutationPreviewError
	require.ErrorAs(t, err, &preview)
	require.Equal(t, "add_endpoint", preview.Preview.Change)
	require.Equal(t, before.Version, preview.Preview.Version)
	require.Equal(t, f.orgSlug+"-second", preview.Preview.EndpointAfter.Slug)
	require.Len(t, preview.Preview.Endpoints, 1)

	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, f.hostedEndpoint.String(), f.orgSlug+"-renamed", "", false)
	require.ErrorAs(t, err, &preview)
	require.Equal(t, "change_endpoint", preview.Preview.Change)
	require.Equal(t, f.orgSlug+"-hosted", preview.Preview.EndpointBefore.Slug)

	require.Equal(t, f.hostedEndpoint.String(), preview.Preview.EndpointAfter.ID)
	require.Equal(t, f.orgSlug+"-renamed", preview.Preview.EndpointAfter.Slug)
	payload := requireConnectionRefusal(t, err, "confirmation_required", "Nothing was changed")
	require.NotNil(t, payload["preview"])

	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, "", f.orgSlug+"-docs#broken", "", false)
	requireConnectionRefusal(t, err, "invalid_request", "lowercase letters")
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, "", f.orgSlug+"-"+strings.Repeat("x", 41), "", false)
	requireConnectionRefusal(t, err, "invalid_request", "at most 40 characters")
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, "", f.orgSlug+"-docs", f.seedDomain(t, ctx, f.principal.OrganizationID, false, false).String(), false)
	requireConnectionRefusal(t, err, "invalid_request", "verified custom domain")
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, "", "unprefixed", "", false)
	requireConnectionRefusal(t, err, "invalid_request", "organization slug")
	other, _ := seedRegistrationLifecycle(t, ctx, f.conn)
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, "", f.orgSlug+"-docs", f.seedDomain(t, ctx, other.OrganizationID, true, true).String(), false)
	requireConnectionRefusal(t, err, "invalid_request", "organization's custom domains")
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, stale(before.Version), "", f.orgSlug+"-second", "", false)
	require.ErrorIs(t, err, ErrMCPConnectionMutationConflict, "a preview of a stale version is refused")

	after := f.settings(t, ctx, MCPConnectionSettingsMCPServer, f.hostedID)
	require.Equal(t, before.Version, after.Version, "a preview writes nothing")
	require.Equal(t, audits, countAudit(t, ctx, f, audit.ActionMcpEndpointCreate))

	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, uuid.NewString(), f.orgSlug+"-renamed", "", false)
	require.ErrorIs(t, err, ErrMCPConnectionMutationMissing, "a preview of another endpoint is not found")
}

func TestSetMCPAddressHostedAddsEndpointsAndReplays(t *testing.T) {
	t.Parallel()
	ctx, f := seedConnectionMutationFixture(t, "platform_mcp_set_address_hosted_add", allowConnectionNetworkAccess{})
	before := f.settings(t, ctx, MCPConnectionSettingsMCPServer, f.hostedID)
	audits := countAudit(t, ctx, f, audit.ActionMcpEndpointCreate)

	input := SetMCPAddressInput{
		ProjectID: f.project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: f.hostedID.String(),
		Slug: f.orgSlug + "-second", ExpectedVersion: before.Version, IdempotencyKey: uuid.NewString(), Confirmed: true,
	}
	out, err := f.mutations.SetAddress(ctx, f.principal, input)
	require.NoError(t, err)
	require.False(t, out.Receipt.Replayed)
	require.Equal(t, "fresh_read_after_commit", out.SnapshotScope)
	require.Len(t, out.Settings.Endpoints, 2, "a hosted server keeps several endpoints")
	require.Equal(t, audits+1, countAudit(t, ctx, f, audit.ActionMcpEndpointCreate))

	replay, err := f.mutations.SetAddress(ctx, f.principal, input)
	require.NoError(t, err)
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, out.Receipt.ID, replay.Receipt.ID)
	require.Len(t, replay.Settings.Endpoints, 2)
	require.Equal(t, audits+1, countAudit(t, ctx, f, audit.ActionMcpEndpointCreate), "a replay writes nothing")

	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, "", f.orgSlug+"-third", "", true)
	require.ErrorIs(t, err, ErrMCPConnectionMutationConflict, "a stale version is refused")

	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, out.Settings.Version, "", "unprefixed-slug", "", true)
	requireConnectionRefusal(t, err, "invalid_request", "organization slug")

	toolset, err := legacyalias.Get(ctx, f.conn, f.project.ID, f.hostedID)
	require.NoError(t, err)
	require.Equal(t, f.orgSlug+"-hosted", toolset.Slug.String, "the alias keeps the endpoint that still holds it")
}

func TestSetMCPAddressHostedChangesEndpointInPlace(t *testing.T) {
	t.Parallel()
	ctx, f := seedConnectionMutationFixture(t, "platform_mcp_set_address_hosted_change", allowConnectionNetworkAccess{})
	before := f.settings(t, ctx, MCPConnectionSettingsMCPServer, f.hostedID)
	audits := countAudit(t, ctx, f, audit.ActionMcpEndpointUpdate)

	out, err := f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, f.hostedEndpoint.String(), f.orgSlug+"-renamed", "", true)
	require.NoError(t, err)
	require.Len(t, out.Settings.Endpoints, 1)
	require.Equal(t, f.hostedEndpoint.String(), out.Settings.Endpoints[0].ID, "the endpoint keeps its identity")
	require.Equal(t, f.orgSlug+"-renamed", out.Settings.Endpoints[0].Slug)
	require.Equal(t, audits+1, countAudit(t, ctx, f, audit.ActionMcpEndpointUpdate))

	toolset, err := legacyalias.Get(ctx, f.conn, f.project.ID, f.hostedID)
	require.NoError(t, err)
	require.Equal(t, f.orgSlug+"-renamed", toolset.Slug.String, "the alias follows its endpoint")
}

func TestSetMCPAddressHostedCustomDomainMustBeLiveAndVerified(t *testing.T) {
	t.Parallel()
	ctx, f := seedConnectionMutationFixture(t, "platform_mcp_set_address_hosted_domain", allowConnectionNetworkAccess{})
	pending := f.seedDomain(t, ctx, f.principal.OrganizationID, false, false)
	version := f.settings(t, ctx, MCPConnectionSettingsMCPServer, f.hostedID).Version

	_, err := f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, version, "", f.orgSlug+"-docs", pending.String(), true)
	requireConnectionRefusal(t, err, "invalid_request", "verified custom domain")

	other, _ := seedRegistrationLifecycle(t, ctx, f.conn)
	foreign := f.seedDomain(t, ctx, other.OrganizationID, true, true)
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, version, "", f.orgSlug+"-docs", foreign.String(), true)
	requireConnectionRefusal(t, err, "invalid_request", "live custom domain")

	_, err = customdomainsrepo.New(f.conn).UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{Verified: true, Activated: true, ProvisionerKind: "local", ID: pending})
	require.NoError(t, err)
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, version, "", "docs", pending.String(), true)
	requireConnectionRefusal(t, err, "invalid_request", "free accounts")

	out, err := f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, version, "", f.orgSlug+"-docs", pending.String(), true)
	require.NoError(t, err)
	require.Len(t, out.Settings.Endpoints, 2)
	var onDomain *MCPConnectionEndpoint
	for i := range out.Settings.Endpoints {
		if out.Settings.Endpoints[i].CustomDomainID == pending.String() {
			onDomain = &out.Settings.Endpoints[i]
		}
	}
	require.NotNil(t, onDomain)
	require.Equal(t, f.orgSlug+"-docs", onDomain.Slug)
}

func TestSetMCPAddressRemoteServerAndGateway(t *testing.T) {
	t.Parallel()
	ctx, f := seedConnectionMutationFixture(t, "platform_mcp_set_address_remote_gateway", allowConnectionNetworkAccess{})
	remote := f.seedRemoteMember(t, ctx, "Remote")
	ctx = f.grantMCP(ctx, f.project.ID, remote, f.hostedID)

	version := f.settings(t, ctx, MCPConnectionSettingsMCPServer, remote).Version
	out, err := f.setAddress(ctx, MCPConnectionSettingsMCPServer, remote, version, "", f.orgSlug+"-remote", "", true)
	require.NoError(t, err)
	require.Len(t, out.Settings.Endpoints, 1)
	out, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, remote, out.Settings.Version, "", f.orgSlug+"-remote-two", "", true)
	require.NoError(t, err)
	require.Len(t, out.Settings.Endpoints, 2)

	gateway, err := f.create(ctx, "Connectors", remote)
	require.NoError(t, err)
	gatewayID := uuid.MustParse(gateway.Gateway.ID)
	settings := f.settings(t, ctx, MCPConnectionSettingsGateway, gatewayID)
	require.Len(t, settings.Endpoints, 1)
	out, err = f.setAddress(ctx, MCPConnectionSettingsGateway, gatewayID, settings.Version, settings.Endpoints[0].ID, f.orgSlug+"-gateway-moved", "", true)
	require.NoError(t, err)
	require.Equal(t, f.orgSlug+"-gateway-moved", out.Settings.Endpoints[0].Slug)

	_, err = f.setAddress(ctx, MCPConnectionSettingsGateway, gatewayID, out.Settings.Version, f.hostedEndpoint.String(), f.orgSlug+"-stolen", "", true)
	require.ErrorIs(t, err, ErrMCPConnectionMutationMissing, "another target's endpoint is not found")
}

func TestSetMCPConnectionMutationRefusesOtherProjectsAndMissingWrite(t *testing.T) {
	t.Parallel()
	ctx, f := seedConnectionMutationFixture(t, "platform_mcp_set_address_scoping", allowConnectionNetworkAccess{})
	version := f.settings(t, ctx, MCPConnectionSettingsMCPServer, f.hostedID).Version

	otherPrincipal, otherProject := seedRegistrationLifecycle(t, ctx, f.conn)
	upstream := remotemcptest.SeedServer(t, ctx, f.conn, remotemcprepo.CreateServerParams{
		ProjectID: otherProject.ID, TransportType: "streamable-http", Url: "https://upstream.example.test/mcp/" + uuid.NewString(),
	})
	server, err := mcpserversrepo.New(f.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: otherProject.ID, Name: pgtype.Text{String: "Elsewhere", Valid: true}, Slug: pgtype.Text{String: "elsewhere", Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: upstream.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, server.ID, version, "", f.orgSlug+"-elsewhere", "", true)
	require.ErrorIs(t, err, ErrMCPConnectionMutationMissing, "a server in another project is not found")
	_, err = f.mutations.SetAddress(ctx, f.principal, SetMCPAddressInput{
		ProjectID: otherProject.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: server.ID.String(),
		Slug: f.orgSlug + "-elsewhere", ExpectedVersion: version, IdempotencyKey: uuid.NewString(), Confirmed: true,
	})
	require.ErrorIs(t, err, ErrMCPConnectionMutationMissing, "another organization's project is not found")
	require.NotEqual(t, otherPrincipal.OrganizationID, f.principal.OrganizationID)

	readOnly := authz.GrantsToContext(ctx, []authz.Grant{
		authz.NewGrant(authz.ScopeOrgAdmin, f.principal.OrganizationID),
		authz.NewGrant(authz.ScopeMCPRead, f.hostedID.String()),
	})
	_, err = f.setAddress(readOnly, MCPConnectionSettingsMCPServer, f.hostedID, version, "", f.orgSlug+"-second", "", true)
	var denied *ExternalAuthorizationError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, string(authz.ScopeMCPWrite), denied.RequiredScope)
	_, err = f.mutations.SetNetworkAccess(readOnly, f.principal, SetMCPNetworkAccessInput{
		ProjectID: f.project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: f.hostedID.String(),
		Mode: "dual", ExpectedVersion: version, IdempotencyKey: uuid.NewString(), Confirmed: true,
	})
	require.ErrorAs(t, err, &denied)
}

func TestSetMCPNetworkAccessChecksEntitlementAndIngress(t *testing.T) {
	t.Parallel()
	setMode := func(ctx context.Context, f connectionMutationFixture, kind MCPConnectionSettingsTargetKind, target uuid.UUID, mode string, confirmed bool) (MCPConnectionMutationOutput, error) {
		return f.mutations.SetNetworkAccess(ctx, f.principal, SetMCPNetworkAccessInput{
			ProjectID: f.project.ID.String(), TargetKind: kind, TargetID: target.String(), Mode: mode,
			ExpectedVersion: f.settings(t, ctx, kind, target).Version, IdempotencyKey: uuid.NewString(), Confirmed: confirmed,
		})
	}

	ctx, denied := seedConnectionMutationFixture(t, "platform_mcp_network_access_denied", networkaccess.DenyAllChecker{})
	denied.seedIngress(t, ctx, "online")
	_, err := setMode(ctx, denied, MCPConnectionSettingsMCPServer, denied.hostedID, "dual", true)
	requireConnectionRefusal(t, err, "invalid_request", "could not be admitted")
	deniedGateway, err := denied.create(ctx, "Connectors", denied.seedRemoteMember(t, ctx, "Remote"))
	require.NoError(t, err)
	_, err = setMode(ctx, denied, MCPConnectionSettingsGateway, uuid.MustParse(deniedGateway.Gateway.ID), "dual", true)
	requireConnectionRefusal(t, err, "invalid_request", "could not be admitted")
	require.Equal(t, "public_only", denied.settings(t, ctx, MCPConnectionSettingsMCPServer, denied.hostedID).NetworkMode)

	ctx, f := seedConnectionMutationFixture(t, "platform_mcp_network_access_ingress", allowConnectionNetworkAccess{})
	_, err = setMode(ctx, f, MCPConnectionSettingsMCPServer, f.hostedID, "private_only", false)
	requireConnectionRefusal(t, err, "invalid_request", "online private network ingress is required")

	_, err = setMode(ctx, f, MCPConnectionSettingsMCPServer, f.hostedID, "dual", true)
	requireConnectionRefusal(t, err, "invalid_request", "online private network ingress is required")

	f.seedIngress(t, ctx, "offline")
	_, err = setMode(ctx, f, MCPConnectionSettingsMCPServer, f.hostedID, "dual", true)
	requireConnectionRefusal(t, err, "invalid_request", "online private network ingress is required")

	require.NoError(t, testrepo.New(f.conn).SetNetworkIngressObservationFixture(ctx, testrepo.SetNetworkIngressObservationFixtureParams{OrganizationID: f.principal.OrganizationID, Status: "online"}))
	_, err = setMode(ctx, f, MCPConnectionSettingsMCPServer, f.hostedID, "private_only", false)
	var preview *connectionMutationPreviewError
	require.ErrorAs(t, err, &preview, "a preview runs the confirmed checks and writes nothing")
	require.Equal(t, "public_only", preview.Preview.NetworkModeBefore)
	require.Equal(t, "private_only", preview.Preview.NetworkModeAfter)
	require.Equal(t, "public_only", f.settings(t, ctx, MCPConnectionSettingsMCPServer, f.hostedID).NetworkMode)
	out, err := setMode(ctx, f, MCPConnectionSettingsMCPServer, f.hostedID, "private_only", true)
	require.NoError(t, err)
	require.Equal(t, "private_only", out.Settings.NetworkMode)

	domain := f.seedDomain(t, ctx, f.principal.OrganizationID, true, true)
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, out.Settings.Version, "", f.orgSlug+"-docs", domain.String(), false)
	requireConnectionRefusal(t, err, "invalid_request", "network ingress namespace")
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, out.Settings.Version, "", f.orgSlug+"-docs", domain.String(), true)
	requireConnectionRefusal(t, err, "invalid_request", "network ingress namespace")

	gateway, err := f.create(ctx, "Connectors", f.seedRemoteMember(t, ctx, "Remote"))
	require.NoError(t, err)
	out, err = setMode(ctx, f, MCPConnectionSettingsGateway, uuid.MustParse(gateway.Gateway.ID), "dual", true)
	require.NoError(t, err)
	require.Equal(t, "dual", out.Settings.NetworkMode)
}

func TestSetMCPAddressRefusesCustomDomainAliasOfOtherProject(t *testing.T) {
	t.Parallel()
	ctx, f := seedConnectionMutationFixture(t, "platform_mcp_set_address_legacy_alias", allowConnectionNetworkAccess{})
	suffix := uuid.NewString()[:8]
	project, err := projectsrepo.New(f.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "victim-" + suffix, Slug: "victim-" + suffix, OrganizationID: f.principal.OrganizationID,
	})
	require.NoError(t, err)
	victim, err := toolsetsrepo.New(f.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: f.principal.OrganizationID, ProjectID: project.ID, Name: "victim-" + suffix, Slug: "victim-" + suffix,
	})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(f.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: victim.ID, ProjectID: project.ID, ToolsetID: uuid.NullUUID{UUID: victim.ID, Valid: true}, Visibility: "public",
	})
	require.NoError(t, err)
	slug := f.orgSlug + "-legacy-" + suffix
	testenv.SetLegacyAlias(t, ctx, f.conn, project.ID, victim.ID, slug, uuid.NullUUID{UUID: f.seedDomain(t, ctx, f.principal.OrganizationID, true, true), Valid: true})

	before := f.settings(t, ctx, MCPConnectionSettingsMCPServer, f.hostedID)
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, "", slug, "", true)
	requireConnectionRefusal(t, err, "conflict", "already")
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, f.hostedEndpoint.String(), slug, "", true)
	requireConnectionRefusal(t, err, "conflict", "already")
	require.Len(t, f.settings(t, ctx, MCPConnectionSettingsMCPServer, f.hostedID).Endpoints, 1)
	_, err = f.setAddress(ctx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, "", f.orgSlug+"-free-"+suffix, "", true)
	require.NoError(t, err, "a free slug is accepted with the same inputs")
}
