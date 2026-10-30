package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// platformCallerContext is the context Platform MCP admission hands a tool:
// the user acting in the organization with no session, enforced through the
// acting surface, carrying their freshly loaded grants.
func platformCallerContext(t *testing.T, ti *testInstance, organizationID, organizationSlug string, actor urn.Principal) context.Context {
	t.Helper()
	ctx := contextvalues.SetActingSurface(t.Context(), contextvalues.ActingSurfacePlatformMCP)
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: organizationID,
		UserID:               actor.ID,
		OrganizationSlug:     organizationSlug,
	}, actor)
	principals, err := authz.ResolveUserPrincipals(ctx, ti.conn, organizationID, actor.ID)
	require.NoError(t, err)
	grants, err := authz.LoadGrants(ctx, ti.conn, organizationID, principals)
	require.NoError(t, err)
	return authz.GrantsToContext(ctx, grants)
}

type platformGatewayResult struct {
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
	Content           []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func callPlatformGateway(t *testing.T, ctx context.Context, ti *testInstance, organizationID, userID, tool string, arguments any) platformGatewayResult {
	t.Helper()
	raw, err := json.Marshal(arguments)
	require.NoError(t, err)
	body, err := ti.service.CallPlatformGatewayTool(ctx, organizationID, userID, tool, raw)
	require.NoError(t, err)
	var result platformGatewayResult
	require.NoError(t, json.Unmarshal(body, &result))
	return result
}

func TestCallPlatformGatewayTool_ListsServersTheUserCanReach(t *testing.T) {
	t.Parallel()
	seedCtx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, seedCtx, ti)
	authCtx, _ := contextvalues.GetAuthContext(seedCtx)
	ctx := platformCallerContext(t, ti, authCtx.ActiveOrganizationID, fx.organizationSlug, urn.NewPrincipal(urn.PrincipalTypeUser, fx.subject.ID))

	result := callPlatformGateway(t, ctx, ti, authCtx.ActiveOrganizationID, fx.subject.ID, metamcp.ToolListServers, map[string]any{})

	require.False(t, result.IsError)
	var listed metamcp.ListServersResult
	require.NoError(t, json.Unmarshal(result.StructuredContent, &listed))
	slugs := make([]string, 0, len(listed.Servers))
	for _, server := range listed.Servers {
		slugs = append(slugs, server.Slug)
	}
	require.ElementsMatch(t, []string{fx.granted, fx.open}, slugs)
}

// A server the caller's grants do not reach reads as unknown, as on the
// gateway itself, and is a refusal to show rather than a failure.
func TestCallPlatformGatewayTool_DeniedServerIsUnknown(t *testing.T) {
	t.Parallel()
	seedCtx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, seedCtx, ti)
	authCtx, _ := contextvalues.GetAuthContext(seedCtx)
	ctx := platformCallerContext(t, ti, authCtx.ActiveOrganizationID, fx.organizationSlug, urn.NewPrincipal(urn.PrincipalTypeUser, fx.subject.ID))

	result := callPlatformGateway(t, ctx, ti, authCtx.ActiveOrganizationID, fx.subject.ID, metamcp.ToolExecuteTool, map[string]any{
		"name":      metamcp.QualifyName(fx.denied, "anything"),
		"arguments": map[string]any{},
	})

	require.True(t, result.IsError)
	require.NotEmpty(t, result.Content)
	require.NotContains(t, result.Content[0].Text, "denied member")
}

// The user named must be the one the context authenticated, acting as
// themselves: another user's id, another organization, or an agent actor
// would resolve credentials that are not the caller's own.
func TestCallPlatformGatewayTool_RefusesAnyOtherIdentity(t *testing.T) {
	t.Parallel()
	seedCtx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, seedCtx, ti)
	authCtx, _ := contextvalues.GetAuthContext(seedCtx)
	organizationID := authCtx.ActiveOrganizationID
	userCtx := platformCallerContext(t, ti, organizationID, fx.organizationSlug, urn.NewPrincipal(urn.PrincipalTypeUser, fx.subject.ID))
	// Only the actor changes: the AuthContext still names this user.
	userAuth, _ := contextvalues.GetAuthContext(userCtx)
	agentCtx := contextvalues.WithAuthenticatedActor(userCtx, userAuth, urn.NewPrincipal(urn.PrincipalTypeAgent, fx.subject.ID))

	call := func(ctx context.Context, organizationID, userID string) error {
		if _, err := ti.service.CallPlatformGatewayTool(ctx, organizationID, userID, metamcp.ToolListServers, json.RawMessage(`{}`)); err != nil {
			return fmt.Errorf("call platform gateway: %w", err)
		}
		return nil
	}
	require.Error(t, call(userCtx, organizationID, "someone-else"), "another user")
	require.Error(t, call(userCtx, "org-elsewhere", fx.subject.ID), "another organization")
	require.Error(t, call(agentCtx, organizationID, fx.subject.ID), "agent actor")
	require.Error(t, call(t.Context(), organizationID, fx.subject.ID), "no identity")
}

func TestCallPlatformGatewayTool_RefusesOtherTools(t *testing.T) {
	t.Parallel()
	seedCtx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, seedCtx, ti)
	authCtx, _ := contextvalues.GetAuthContext(seedCtx)
	ctx := platformCallerContext(t, ti, authCtx.ActiveOrganizationID, fx.organizationSlug, urn.NewPrincipal(urn.PrincipalTypeUser, fx.subject.ID))

	_, err := ti.service.CallPlatformGatewayTool(ctx, authCtx.ActiveOrganizationID, fx.subject.ID, metamcp.ToolConnectionStatus, json.RawMessage(`{}`))

	require.Error(t, err)
}

// Platform MCP's own use of the gateway is on in an organization that never
// configured it, and off once an administrator turns it off.
func TestCallPlatformGatewayTool_GatewayIsOnUntilTurnedOff(t *testing.T) {
	t.Parallel()
	seedCtx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, seedCtx, ti)
	authCtx, _ := contextvalues.GetAuthContext(seedCtx)
	organizationID := authCtx.ActiveOrganizationID
	ctx := platformCallerContext(t, ti, organizationID, fx.organizationSlug, urn.NewPrincipal(urn.PrincipalTypeUser, fx.subject.ID))
	setting := string(productfeatures.FeatureOrganizationGatewayDisabled)
	_, err := testrepo.New(ti.conn).ClearOrganizationFeatureSettingFixture(ctx, testrepo.ClearOrganizationFeatureSettingFixtureParams{OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)})
	require.NoError(t, err)

	result := callPlatformGateway(t, ctx, ti, organizationID, fx.subject.ID, metamcp.ToolListServers, map[string]any{})
	require.False(t, result.IsError)

	_, err = featuresrepo.New(ti.conn).EnableFeature(ctx, featuresrepo.EnableFeatureParams{OrganizationID: organizationID, FeatureName: setting})
	require.NoError(t, err)
	result = callPlatformGateway(t, ctx, ti, organizationID, fx.subject.ID, metamcp.ToolListServers, map[string]any{})
	require.True(t, result.IsError)
	require.Contains(t, result.Content[0].Text, "turned_off")
}

// One search ranks tools across every server the caller reaches and never
// reads a server their grants do not reach.
func TestCallPlatformGatewayTool_SearchesReachableServers(t *testing.T) {
	t.Parallel()
	seedCtx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, seedCtx, ti)
	authCtx, _ := contextvalues.GetAuthContext(seedCtx)
	ctx := platformCallerContext(t, ti, authCtx.ActiveOrganizationID, fx.organizationSlug, urn.NewPrincipal(urn.PrincipalTypeUser, fx.subject.ID))

	result := callPlatformGateway(t, ctx, ti, authCtx.ActiveOrganizationID, fx.subject.ID, mcp.PlatformGatewaySearchTools, map[string]any{"query": "", "limit": 50})

	require.False(t, result.IsError, result.Content)
	var page mcp.PlatformGatewaySearchResult
	require.NoError(t, json.Unmarshal(result.StructuredContent, &page))
	// The fixture's members are remote servers with no live upstream, so each
	// search reads and fails exactly the members the caller reaches.
	searched := make([]string, 0, len(page.FailedMembers)+len(page.Items))
	for _, failed := range page.FailedMembers {
		searched = append(searched, failed.Server)
	}
	for _, item := range page.Items {
		searched = append(searched, item.Server)
	}
	require.ElementsMatch(t, []string{fx.granted, fx.open}, searched)
	require.True(t, page.Incomplete)

	denied := callPlatformGateway(t, ctx, ti, authCtx.ActiveOrganizationID, fx.subject.ID, mcp.PlatformGatewaySearchTools, map[string]any{"query": "", "server": fx.denied})
	require.True(t, denied.IsError)
}
