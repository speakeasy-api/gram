package platformmcp

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	genassistants "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
	"testing"
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
	require.Contains(t, a.Description, "project:read")
	require.Contains(t, a.Description, "OAuth consent")
	require.NotContains(t, string(a.InputSchema), "token")
}

func TestAssistantIdentityInspectionUsesExactAuthorizedReadAndSafeProjection(t *testing.T) {
	t.Parallel()
	project, id := uuid.New(), uuid.New()
	principal := Principal{OrganizationID: "org-example", UserID: "user-example"}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID})
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
