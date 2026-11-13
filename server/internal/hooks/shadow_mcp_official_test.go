package hooks

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type officialServerSeed struct {
	Name       string
	RemoteURL  string
	Visibility string
	// EndpointSlug gives the server its own address; empty leaves it
	// without one.
	EndpointSlug string
}

// seedOfficialServer creates what the dashboard creates when a remote MCP
// server is added: the remote source, the MCP server in front of it, and
// (optionally) the endpoint agents connect to.
func seedOfficialServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, seed officialServerSeed) uuid.UUID {
	t.Helper()

	visibility := seed.Visibility
	if visibility == "" {
		visibility = "private"
	}
	remote, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          conv.ToPGText(seed.Name),
		Slug:          conv.ToPGText("remote-" + uuid.NewString()),
		TransportType: "streamable-http",
		Url:           seed.RemoteURL,
	})
	require.NoError(t, err)

	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             projectID,
		Name:                  conv.ToPGText(seed.Name),
		Slug:                  conv.ToPGText("server-" + uuid.NewString()),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remote.ID, Valid: true},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            visibility,
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	if seed.EndpointSlug != "" {
		_, err := mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
			ProjectID:       projectID,
			CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			McpServerID:     uuid.NullUUID{UUID: server.ID, Valid: true},
			MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Slug:            seed.EndpointSlug,
		})
		require.NoError(t, err)
	}
	return server.ID
}

func TestEnforceShadowMCPToolAccess_OfficialServerBlocksDirectUseDespiteBypassGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	serverURL := "https://tracker.example.com/mcp"
	seedOfficialServer(t, ctx, ti.conn, *authCtx.ProjectID, officialServerSeed{
		Name: "Tracker", RemoteURL: serverURL, Visibility: "", EndpointSlug: "tracker-" + uuid.NewString(),
	})

	// An approval of the direct URL from before the server was onboarded.
	policyID := uuid.NewString()
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = serverURL
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Principals: []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:   selector,
	}))

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		blockAllTestPolicy(policyID),
		"list_issues",
		shadowmcp.AccessEvidence{FullURL: serverURL, URLHost: "", ServerIdentity: "tracker"},
	)

	require.True(t, denied, "an onboarded server is blocked for direct use even where the direct URL was approved")
	require.Contains(t, detail, `official version "Tracker"`)
}

func TestEnforceShadowMCPToolAccess_OfficialServerBlocksDirectUseUnderAllowAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// The stored URL ends in a slash and the observed one does not: they
	// name one upstream.
	seedOfficialServer(t, ctx, ti.conn, *authCtx.ProjectID, officialServerSeed{
		Name: "Wiki", RemoteURL: "https://wiki.example.com/mcp/", Visibility: "", EndpointSlug: "wiki-" + uuid.NewString(),
	})

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		allowAllTestPolicy(uuid.NewString()),
		"search",
		shadowmcp.AccessEvidence{FullURL: "https://wiki.example.com/mcp", URLHost: "", ServerIdentity: "wiki"},
	)

	require.True(t, denied)
	require.Contains(t, detail, `official version "Wiki"`)
}

func TestEnforceShadowMCPToolAccess_OfficialServerThatCannotBeReachedDoesNotBlock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// No endpoint: there is nowhere to send people yet.
	seedOfficialServer(t, ctx, ti.conn, *authCtx.ProjectID, officialServerSeed{
		Name: "No Address", RemoteURL: "https://no-address.example.com/mcp", Visibility: "", EndpointSlug: "",
	})
	// Disabled: it has an address, but nobody can use it.
	seedOfficialServer(t, ctx, ti.conn, *authCtx.ProjectID, officialServerSeed{
		Name: "Disabled", RemoteURL: "https://disabled.example.com/mcp", Visibility: "disabled", EndpointSlug: "disabled-" + uuid.NewString(),
	})

	for _, directURL := range []string{"https://no-address.example.com/mcp", "https://disabled.example.com/mcp"} {
		detail, denied := ti.service.enforceShadowMCPToolAccess(
			ctx,
			authCtx.ActiveOrganizationID,
			authCtx.ProjectID.String(),
			authCtx.UserID,
			allowAllTestPolicy(uuid.NewString()),
			"do_thing",
			shadowmcp.AccessEvidence{FullURL: directURL, URLHost: "", ServerIdentity: "server"},
		)
		require.False(t, denied, directURL)
		require.Empty(t, detail, directURL)
	}
}

func TestRenderShadowMCPUserBlockReason_SendsAgentToOfficialServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	serverURL := "https://tracker.example.com/mcp"
	endpointSlug := "tracker-" + uuid.NewString()
	seedOfficialServer(t, ctx, ti.conn, *authCtx.ProjectID, officialServerSeed{
		Name: "Tracker", RemoteURL: serverURL, Visibility: "", EndpointSlug: endpointSlug,
	})

	reason := ti.service.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		RequesterUserID: authCtx.UserID,
		UserMessage:     nil,
		AuditReason:     "Speakeasy blocked this tool call",
		Evidence:        shadowmcp.AccessEvidence{FullURL: serverURL, URLHost: "", ServerIdentity: "tracker"},
		ToolName:        "list_issues",
		ToolInput:       nil,
		RiskPolicyID:    uuid.NewString(),
		PolicyName:      "Shadow MCP Block",
	})

	require.Contains(t, reason, "Speakeasy blocked this tool call")
	require.Contains(t, reason, `official version of this MCP server: "Tracker"`)
	require.Contains(t, reason, "https://localhost:8080/mcp/"+endpointSlug)
	require.NotContains(t, reason, "Request access", "the answer is the official server, not a request for the direct one")
}

func TestRenderShadowMCPUserBlockReason_SendsAgentToGatewayThatServesOfficialServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	serverURL := "https://chat.example.com/mcp"
	serverID := seedOfficialServer(t, ctx, ti.conn, *authCtx.ProjectID, officialServerSeed{
		Name: "Chat", RemoteURL: serverURL, Visibility: "", EndpointSlug: "chat-" + uuid.NewString(),
	})

	gatewayID, err := testrepo.New(ti.conn).CreateMCPGatewayFixture(ctx, testrepo.CreateMCPGatewayFixtureParams{
		ID:             uuid.New(),
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Team Gateway",
	})
	require.NoError(t, err)
	_, err = metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       *authCtx.ProjectID,
		MetaMcpServerID: gatewayID,
		McpServerID:     serverID,
		SortOrder:       0,
	})
	require.NoError(t, err)
	gatewaySlug := "team-gateway-" + uuid.NewString()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		MetaMcpServerID: uuid.NullUUID{UUID: gatewayID, Valid: true},
		Slug:            gatewaySlug,
	})
	require.NoError(t, err)

	reason := ti.service.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		RequesterUserID: authCtx.UserID,
		UserMessage:     nil,
		AuditReason:     "Speakeasy blocked this tool call",
		Evidence:        shadowmcp.AccessEvidence{FullURL: serverURL, URLHost: "", ServerIdentity: "chat"},
		ToolName:        "post_message",
		ToolInput:       nil,
		RiskPolicyID:    uuid.NewString(),
		PolicyName:      "Shadow MCP Block",
	})

	require.Contains(t, reason, `"Chat", through the gateway "Team Gateway"`)
	require.Contains(t, reason, "https://localhost:8080/mcp/"+gatewaySlug, "a gateway member is reached at the gateway's address")
}

func TestRenderShadowMCPUserBlockReason_KeepsRequestLinkWithoutOfficialServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	reason := ti.service.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		RequesterUserID: authCtx.UserID,
		UserMessage:     nil,
		AuditReason:     "Speakeasy blocked this tool call",
		Evidence:        shadowmcp.AccessEvidence{FullURL: "https://unknown.example.com/mcp", URLHost: "", ServerIdentity: "unknown"},
		ToolName:        "do_thing",
		ToolInput:       nil,
		RiskPolicyID:    uuid.NewString(),
		PolicyName:      "Shadow MCP Block",
	})

	require.NotContains(t, reason, "official version")
}
