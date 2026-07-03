package hooks

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
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
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Principals: []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:   selector,
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
		uuid.NewString(),
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
		uuid.NewString(),
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
		uuid.NewString(),
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: "local-stdio"},
	)

	require.True(t, denied)
	require.Contains(t, detail, "not Gram-hosted")
}
