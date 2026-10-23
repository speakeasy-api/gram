package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	gatewaygen "github.com/speakeasy-api/gram/server/gen/gateway_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

type gatewaySessionManagementFunc func(context.Context, *gatewaygen.ListGatewaySessionsPayload) (*gatewaygen.GatewaySessionsResult, error)

func (f gatewaySessionManagementFunc) ListGatewaySessions(ctx context.Context, p *gatewaygen.ListGatewaySessionsPayload) (*gatewaygen.GatewaySessionsResult, error) {
	return f(ctx, p)
}

func TestGatewaySessionToolFilteringAndPrivacy(t *testing.T) {
	t.Parallel()
	principal := Principal{OrganizationID: "test-org", UserID: "test-user"}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: new(uuid.New())})
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)})
	gateway := uuid.NewString()
	input := ListGatewaySessionsInput{Scope: "gateway", GatewayID: &gateway, Search: new("caller-provided"), Status: "expired", Sort: "expiry", ServerID: new(uuid.NewString()), ProviderID: new(uuid.NewString()), CreatedAfter: new("2026-01-01T00:00:00Z"), CreatedBefore: new("2026-01-02T00:00:00Z"), Cursor: new("previous-page"), Limit: 10}
	calls := 0
	service := gatewaySessionManagementFunc(func(_ context.Context, payload *gatewaygen.ListGatewaySessionsPayload) (*gatewaygen.GatewaySessionsResult, error) {
		calls++
		require.Equal(t, input.GatewayID, payload.GatewayID)
		require.Equal(t, input.Search, payload.Search)
		require.Equal(t, input.Status, payload.Status)
		require.Equal(t, input.Sort, payload.Sort)
		require.Equal(t, input.ServerID, payload.ServerID)
		require.Equal(t, input.ProviderID, payload.ProviderID)
		require.Equal(t, input.CreatedAfter, payload.CreatedAfter)
		require.Equal(t, input.CreatedBefore, payload.CreatedBefore)
		require.Equal(t, input.Cursor, payload.Cursor)
		require.Equal(t, input.Limit, payload.Limit)
		return &gatewaygen.GatewaySessionsResult{Items: []*types.UserSession{{ID: "authorization-id", SubjectType: "user", SubjectUrn: "private-subject", SubjectDisplayName: new("private-name"), ClientName: new("private-client"), Jti: "private-jti", Workload: &types.UserSessionWorkload{}, Upstreams: []*types.UserSessionUpstream{{RemoteSessionID: "private-upstream"}}}}, NextCursor: new("next-page")}, nil
	})
	for _, scope := range []string{"gateway", "organization"} {
		input.Scope = scope
		if scope == "organization" {
			input.GatewayID = nil
		}
		output, err := listGatewaySessions(ctx, service, principal, input)
		require.NoError(t, err)
		require.Equal(t, new("next-page"), output.NextCursor)
		require.Equal(t, 1, output.Items[0].UpstreamAccounts)
		encoded, err := json.Marshal(output)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private-")
		require.Contains(t, string(encoded), "authorization-id")
	}
	require.Equal(t, 2, calls)
	for _, grants := range [][]authz.Grant{nil, {authz.NewGrant(authz.ScopeOrgRead, principal.OrganizationID)}, {authz.NewGrant(authz.ScopeOrgAdmin, "other-org")}} {
		_, err := listGatewaySessions(authz.GrantsToContext(ctx, grants), service, principal, input)
		require.ErrorIs(t, err, ErrForbidden)
	}
	mismatched := principal
	mismatched.OrganizationID = "other-org"
	_, err := listGatewaySessions(ctx, service, mismatched, input)
	require.ErrorIs(t, err, ErrForbidden)
	for _, invalid := range []ListGatewaySessionsInput{{}, {Scope: "gateway"}, {Scope: "organization", GatewayID: &gateway}, {Scope: "organization", Limit: 101}} {
		_, err := listGatewaySessions(ctx, service, principal, invalid)
		require.Error(t, err)
	}
	require.Equal(t, 2, calls, "refused requests must not reach session inventory")
	_, err = listGatewaySessions(ctx, nil, principal, input)
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestGatewaySessionToolContract(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerGatewaySessionTool(live, gatewaySessionManagementFunc(func(context.Context, *gatewaygen.ListGatewaySessionsPayload) (*gatewaygen.GatewaySessionsResult, error) {
		return nil, ErrUnavailable
	}))
	registerGatewaySessionTool(unavailable, nil)
	a := descriptorByName(t, live, listGatewaySessionsToolName)
	b := descriptorByName(t, unavailable, listGatewaySessionsToolName)
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, ExternalAuthorizationOrgAdmin, a.Meta.Authorization)
	require.Equal(t, ProjectScopeNone, a.Meta.ProjectScope)
	require.Empty(t, live.For(AudienceAssistant))
	for _, field := range []string{"scope", "gateway_id", "search", "server_id", "provider_id", "status", "created_after", "created_before", "sort", "cursor", "limit"} {
		require.Contains(t, string(a.InputSchema), field)
	}
	principal := Principal{OrganizationID: "test-org", UserID: "test-user"}
	adminCtx := authz.GrantsToContext(t.Context(), []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)})
	memberCtx := authz.GrantsToContext(t.Context(), []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, principal.OrganizationID)})
	tools := []*mcp.Tool{{Name: listGatewaySessionsToolName}}
	require.Len(t, live.FilterExternalTools(adminCtx, principal, tools), 1)
	require.Empty(t, live.FilterExternalTools(memberCtx, principal, tools))
}

func TestGatewaySessionToolInvocationRefusals(t *testing.T) {
	t.Parallel()
	principal := Principal{OrganizationID: "test-org", UserID: "test-user"}
	ctx := ContextWithPrincipal(contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}), principal)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)})
	reg := newRegistrar(newTestMCPServer())
	registerGatewaySessionTool(reg, gatewaySessionManagementFunc(func(context.Context, *gatewaygen.ListGatewaySessionsPayload) (*gatewaygen.GatewaySessionsResult, error) {
		return nil, oops.E(oops.CodeBadRequest, nil, "private internal details")
	}))
	descriptor := descriptorByName(t, reg, listGatewaySessionsToolName)
	for _, arguments := range []string{`{"scope":"invalid"}`, `{"scope":"organization","cursor":"stale"}`} {
		_, err := descriptor.Invoke(ctx, json.RawMessage(arguments))
		refusal, ok := errors.AsType[*ToolRefusalError](err)
		require.True(t, ok)
		require.Contains(t, refusal.Payload, `"code":"invalid_request"`)
		require.NotContains(t, refusal.Payload, "private")
	}
	_, err := descriptor.Invoke(authz.GrantsToContext(ctx, nil), json.RawMessage(`{"scope":"organization"}`))
	refusal, ok := errors.AsType[*ToolRefusalError](err)
	require.True(t, ok)
	require.Contains(t, refusal.Payload, `"code":"permission_denied"`)
}
