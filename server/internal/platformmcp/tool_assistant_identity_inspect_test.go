package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	genassistants "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type identityInspectionStub struct {
	assistantIdentityManagementFunc
	read func(context.Context, *genassistants.GetAssistantPayload) (*types.Assistant, error)
}

func (s identityInspectionStub) GetAssistant(ctx context.Context, p *genassistants.GetAssistantPayload) (*types.Assistant, error) {
	return s.read(ctx, p)
}

func TestAssistantIdentityInspectionContract(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	registerAssistantIdentityTool(live, &assistantIdentityService{})
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerAssistantIdentityTool(unavailable, nil)
	a, b := descriptorByName(t, live, inspectAssistantIdentityToolName), descriptorByName(t, unavailable, inspectAssistantIdentityToolName)
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, ExternalAuthorizationMember, a.Meta.Authorization)
	require.Equal(t, externalOnly, a.Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
	require.Equal(t, discoveryProjectRead, a.Meta.DiscoveryScopes)
	require.Contains(t, a.Description, "project:read")
	require.Contains(t, a.Description, "OAuth consent")
	require.NotContains(t, string(a.InputSchema), "token")
	ctx := contextWithPrincipal(t.Context(), Principal{OrganizationID: "test-org", UserID: "test-user"})
	_, err := b.Invoke(ctx, []byte(`{"project_id":"`+uuid.NewString()+`","assistant_id":"`+uuid.NewString()+`"}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, unavailableCode)
	require.Contains(t, refusal.Payload, "inspection")
	require.NotContains(t, refusal.Payload, "upgrade")
}

func TestAssistantIdentityInspectionUsesExactAuthorizedReadAndSafeProjection(t *testing.T) {
	t.Parallel()
	project, id := uuid.New(), uuid.New()
	principal := Principal{OrganizationID: "org-example", UserID: "user-example"}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID})
	grants := []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, project.String())}
	ctx = authz.GrantsToContext(ctx, grants)
	calls := 0
	service := &assistantIdentityService{
		resolveProject: func(_ context.Context, org string, in FindMCPInput) (ResolvedProject, error) {
			require.Equal(t, principal.OrganizationID, org)
			require.Equal(t, project.String(), in.ProjectID)
			return ResolvedProject{ID: project, Slug: "example"}, nil
		},
		management: identityInspectionStub{assistantIdentityManagementFunc: func(context.Context, *genassistants.UpgradeAssistantIdentityPayload) (*types.Assistant, error) {
			t.Fatal("inspection must never provision")
			return nil, nil
		}, read: func(ctx context.Context, p *genassistants.GetAssistantPayload) (*types.Assistant, error) {
			calls++
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.Equal(t, project, *ac.ProjectID)
			gotGrants, ok := authz.GrantsFromContext(ctx)
			require.True(t, ok)
			require.Equal(t, grants, gotGrants)
			require.Equal(t, id.String(), p.ID)
			return &types.Assistant{ID: id.String(), ProjectID: project.String(), Instructions: "private-system-prompt", IdentityDiagnostics: &types.AssistantIdentityDiagnostics{Health: "suspended", Bindings: []*types.AssistantIdentityBinding{}}}, nil
		}},
	}
	result, err := service.inspect(ctx, principal, InspectAssistantIdentityInput{ProjectID: project.String(), AssistantID: id.String()})
	require.NoError(t, err)
	require.Equal(t, "suspended", result.Diagnostics.Health)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-system-prompt")
	_, err = service.inspect(ctx, principal, InspectAssistantIdentityInput{ProjectID: "", AssistantID: id.String()})
	require.Error(t, err)
	require.Equal(t, 1, calls)
	service.resolveProject = func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
		return ResolvedProject{ID: uuid.New()}, nil
	}
	_, err = service.inspect(ctx, principal, InspectAssistantIdentityInput{ProjectID: project.String(), AssistantID: id.String()})
	require.ErrorIs(t, err, ErrForbidden)
	require.Equal(t, 1, calls)
	service.resolveProject = func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
		return ResolvedProject{}, oops.C(oops.CodeForbidden)
	}
	_, err = service.inspect(ctx, principal, InspectAssistantIdentityInput{ProjectID: project.String(), AssistantID: id.String()})
	require.Error(t, err)
	require.Equal(t, 1, calls)
}

func TestAssistantIdentityInspectionRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		err     error
		code    string
		message string
	}{
		{"unavailable", ErrUnavailable, unavailableCode, "inspection is temporarily unavailable"},
		{"invalid", oops.C(oops.CodeInvalid), "invalid_request", "UUIDs to inspect"},
		{"bad request", oops.C(oops.CodeBadRequest), "invalid_request", "UUIDs to inspect"},
		{"forbidden", oops.C(oops.CodeForbidden), "not_found", "not available to you"},
		{"unauthorized", oops.C(oops.CodeUnauthorized), "not_found", "not available to you"},
		{"hidden", ErrForbidden, "not_found", "not available to you"},
		{"not found", oops.C(oops.CodeNotFound), "not_found", "not available to you"},
		{"conflict", oops.C(oops.CodeConflict), "conflict", "cannot be inspected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, ok := assistantIdentityInspectionToolResult(tc.err)
			require.True(t, ok)
			require.True(t, result.IsError)
			content, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			payload := content.Text
			require.Contains(t, payload, tc.code)
			require.Contains(t, payload, tc.message)
			require.NotContains(t, payload, "upgrade")
			require.NotContains(t, payload, "confirm")
			require.NotContains(t, payload, "project:write")
		})
	}
}
