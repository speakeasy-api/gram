package hooks

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
		policyID,
		map[string]any{},
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: serverURL, URLHost: "", ServerIdentity: "blocked-server"},
	)

	require.False(t, denied)
	require.Empty(t, detail)
}

func TestEnforceShadowMCPToolAccess_BypassGrantRequiresServerURLTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	policyID := uuid.NewString()
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = "https://blocked.example.com/mcp"
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
		policyID,
		map[string]any{},
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: "local-server"},
	)

	require.True(t, denied)
	require.Contains(t, detail, "missing required")
}

func TestEnforceShadowMCPToolAccess_LegacyAccessRuleAllowsBlockedCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	policyID := uuid.NewString()
	serverURL := "https://legacy-blocked.example.com/mcp"
	createHookAccessRule(
		t,
		ctx,
		ti,
		authCtx.ProjectID.String(),
		accesscontrol.AccessScopeProject,
		accesscontrol.DispositionAllowed,
		accesscontrol.MatchKindFullURL,
		serverURL,
		"Legacy blocked server",
	)

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		policyID,
		map[string]any{},
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: serverURL, URLHost: "", ServerIdentity: "legacy-blocked-server"},
	)

	require.False(t, denied)
	require.Empty(t, detail)
}
