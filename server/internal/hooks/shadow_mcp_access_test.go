package hooks

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestClaudeShadowMCPEvidence_DerivesServerIdentityOnly(t *testing.T) {
	t.Parallel()

	evidence := claudeShadowMCPEvidence("mcp__claude_ai_Calendly__authenticate")

	require.Empty(t, evidence.FullURL)
	require.Empty(t, evidence.URLHost)
	require.Equal(t, "claude_ai_Calendly", evidence.ServerIdentity)
}

func TestCursorShadowMCPEvidence_DerivesURLAndServerIdentity(t *testing.T) {
	t.Parallel()

	serverURL := "https://mcp.calendly.com/sse"
	toolName := "MCP:authenticate"
	evidence := cursorShadowMCPEvidence(&gen.CursorPayload{
		ToolName: &toolName,
		URL:      &serverURL,
	})

	require.Equal(t, serverURL, evidence.FullURL)
	require.Empty(t, evidence.URLHost)
	require.Equal(t, "mcp.calendly.com", evidence.ServerIdentity)
}

func TestEnforceShadowMCPToolAccess_BypassGrantAllowsBlockedCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	policyID := uuid.NewString()
	serverURL := "https://blocked.example.com/mcp"
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = serverURL
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		OrganizationID: authCtx.ActiveOrganizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
		Principals:     []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:       selector,
	}))

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		blockAllTestPolicy(policyID),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: serverURL, URLHost: "", ServerIdentity: "blocked-server"},
	)

	require.False(t, denied)
	require.Empty(t, detail)
}

func TestEnforceShadowMCPToolAccess_URLScopedBypassGrantDoesNotAllowIdentityOnlyTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	policyID := uuid.NewString()
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = "https://blocked.example.com/mcp"
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		OrganizationID: authCtx.ActiveOrganizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
		Principals:     []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:       selector,
	}))

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		blockAllTestPolicy(policyID),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: "local-server"},
	)

	require.True(t, denied)
	require.Contains(t, detail, "not Gram-hosted")
}

func TestEnforceShadowMCPToolAccess_IdentityScopedBypassGrantAllowsIdentityOnlyTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	policyID := uuid.NewString()
	serverIdentity := "local-server"
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerIdentity] = serverIdentity
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		OrganizationID: authCtx.ActiveOrganizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
		Principals:     []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:       selector,
	}))

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		blockAllTestPolicy(policyID),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: serverIdentity},
	)

	require.False(t, denied)
	require.Empty(t, detail)
}

func TestEnforceShadowMCPToolAccess_WholePolicyBypassGrantAllowsIdentityOnlyTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	policyID := uuid.NewString()
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		OrganizationID: authCtx.ActiveOrganizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
		Principals:     []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:       selector,
	}))

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		blockAllTestPolicy(policyID),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: "local-server"},
	)

	require.False(t, denied)
	require.Empty(t, detail)
}

func TestCanBypassPolicy_EmptyEvidenceDoesNotUseWholePolicyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	policyID := uuid.NewString()
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		OrganizationID: authCtx.ActiveOrganizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
		Principals:     []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:       selector,
	}))

	target, allowed := ti.service.canBypassPolicy(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.UserID,
		policyID,
		shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: ""},
		"do_thing",
	)

	require.False(t, allowed)
	require.Nil(t, target)
}

func TestEnforceShadowMCPToolAccess_GramHostedURLAllowed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		blockAllTestPolicy(uuid.NewString()),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "https://app.getgram.ai/mcp/example", URLHost: "", ServerIdentity: "example"},
	)

	require.False(t, denied)
	require.Empty(t, detail)
}

func TestEnforceShadowMCPToolAccess_NonGramURLBlocked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		blockAllTestPolicy(uuid.NewString()),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "https://mcp.shadow.example/mcp", URLHost: "", ServerIdentity: "mcp.shadow.example"},
	)

	require.True(t, denied)
	require.Contains(t, detail, "not Gram-hosted")
}

func TestEnforceShadowMCPToolAccess_NoURLServerBlocked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		blockAllTestPolicy(uuid.NewString()),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: "local-stdio"},
	)

	require.True(t, denied)
	require.Contains(t, detail, "not Gram-hosted")
}

// blockAllTestPolicy fabricates the block_all policy shape the enforcement
// path receives from LookupShadowMCPBlockingPolicy.
func blockAllTestPolicy(policyID string) *risk.ShadowMCPPolicy {
	return &risk.ShadowMCPPolicy{
		ID:          policyID,
		Name:        "Shadow MCP Block",
		Version:     1,
		UserMessage: nil,
		Disposition: risk.ShadowMCPDispositionBlockAll,
		BlockedURLs: nil,
	}
}

func allowAllTestPolicy(policyID string, blockedURLs ...string) *risk.ShadowMCPPolicy {
	return &risk.ShadowMCPPolicy{
		ID:          policyID,
		Name:        "Shadow MCP Allow All",
		Version:     1,
		UserMessage: nil,
		Disposition: risk.ShadowMCPDispositionAllowAll,
		BlockedURLs: blockedURLs,
	}
}

func TestEnforceShadowMCPToolAccess_AllowAllPermitsNonBlockedURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		allowAllTestPolicy(uuid.NewString(), "https://sketchy.example.com/mcp"),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "https://fine.example.com/mcp", URLHost: "", ServerIdentity: "fine.example.com"},
	)

	require.False(t, denied)
	require.Empty(t, detail)
}

func TestEnforceShadowMCPToolAccess_AllowAllBlocksListedURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// The evidence URL matches the blocked list after canonicalization
	// (scheme-default port and query string stripped, host lowercased).
	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		allowAllTestPolicy(uuid.NewString(), "https://sketchy.example.com/mcp"),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "HTTPS://SKETCHY.EXAMPLE.COM:443/mcp?token=x", URLHost: "", ServerIdentity: "sketchy.example.com"},
	)

	require.True(t, denied)
	require.Contains(t, detail, "blocked by policy")
	require.Contains(t, detail, "https://sketchy.example.com/mcp")
}

func TestEnforceShadowMCPToolAccess_AllowAllPermitsIdentityOnlyEvidence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Local stdio servers have no URL, so a URL blocklist can never match
	// them; under permit-by-default they are allowed.
	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		allowAllTestPolicy(uuid.NewString(), "https://sketchy.example.com/mcp"),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: "local-stdio"},
	)

	require.False(t, denied)
	require.Empty(t, detail)
}

func TestEnforceShadowMCPToolAccess_AllowAllIgnoresBypassGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// A bypass grant for the blocked URL must not override the blocked list:
	// grants are a block_all concept.
	policyID := uuid.NewString()
	serverURL := "https://sketchy.example.com/mcp"
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = serverURL
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		OrganizationID: authCtx.ActiveOrganizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
		Principals:     []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:       selector,
	}))

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		allowAllTestPolicy(policyID, serverURL),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: serverURL, URLHost: "", ServerIdentity: "sketchy.example.com"},
	)

	require.True(t, denied)
	require.Contains(t, detail, "blocked by policy")
}
