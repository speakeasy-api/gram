package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	genassistants "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

type assistantRuntimeManagementStub struct {
	assistantIdentityManagementFunc
	update func(context.Context, *genassistants.UpdateAssistantPayload) (*types.Assistant, error)
}

func (s assistantRuntimeManagementStub) GetAssistant(ctx context.Context, p *genassistants.GetAssistantPayload) (*types.Assistant, error) {
	auth, _ := contextvalues.GetAuthContext(ctx)
	return &types.Assistant{ID: p.ID, ProjectID: auth.ProjectID.String(), RuntimeVersion: 2}, nil
}

func (s assistantRuntimeManagementStub) UpdateAssistant(ctx context.Context, p *genassistants.UpdateAssistantPayload) (*types.Assistant, error) {
	return s.update(ctx, p)
}

func TestAssistantRuntimeSelection(t *testing.T) {
	t.Parallel()
	principal := Principal{OrganizationID: "test-org", UserID: "test-user"}
	projectID, assistantID := uuid.New(), uuid.New()
	auth := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), auth, actor)
	calls := 0
	var endpointErr error
	service := &AssistantIdentityService{
		organizations: riskMutationOrganizationResolver{slug: "test-org"},
		resolveProject: func(_ context.Context, org string, in FindMCPInput) (ResolvedProject, error) {
			require.Equal(t, principal.OrganizationID, org)
			require.Equal(t, projectID.String(), in.ProjectID)
			return ResolvedProject{ID: projectID, Slug: "exact-project"}, nil
		},
		management: assistantRuntimeManagementStub{update: func(ctx context.Context, p *genassistants.UpdateAssistantPayload) (*types.Assistant, error) {
			calls++
			scoped, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.Equal(t, projectID, *scoped.ProjectID)
			gotActor, ok := contextvalues.AuthenticatedActor(ctx)
			require.True(t, ok)
			require.Equal(t, actor, gotActor)
			require.Equal(t, assistantID.String(), p.ID)
			require.Equal(t, 3, *p.RuntimeVersion)
			require.Equal(t, 2, *p.ExpectedRuntimeVersion)
			require.Nil(t, p.Name)
			require.Nil(t, p.Instructions)
			require.Nil(t, p.Toolsets)
			if endpointErr != nil {
				return nil, endpointErr
			}
			return &types.Assistant{ID: p.ID, ProjectID: projectID.String(), RuntimeVersion: 3, Instructions: "private"}, nil
		}},
	}
	input := SelectAssistantRuntimeInput{ProjectID: projectID.String(), AssistantID: assistantID.String(), RuntimeVersion: 3, ExpectedRuntimeVersion: 2, Confirmed: true}
	out, err := service.selectRuntime(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, 3, out.RuntimeVersion)
	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.Nil(t, auth.ProjectID)
	for _, code := range []oops.Code{oops.CodeConflict, oops.CodeForbidden} {
		endpointErr = oops.C(code)
		_, err = service.selectRuntime(ctx, principal, input)
		require.ErrorIs(t, err, endpointErr)
	}
	before := calls
	preview := input
	preview.Confirmed = false
	preview.ExpectedRuntimeVersion = 0
	out, err = service.selectRuntime(ctx, principal, preview)
	require.NoError(t, err)
	require.Equal(t, 2, out.RuntimeVersion)
	require.Equal(t, before, calls)

	for _, invalid := range []SelectAssistantRuntimeInput{
		{ProjectID: input.ProjectID, AssistantID: "invalid", RuntimeVersion: 3, ExpectedRuntimeVersion: 2},
		{ProjectID: input.ProjectID, AssistantID: input.AssistantID, RuntimeVersion: 1, ExpectedRuntimeVersion: 2, Confirmed: true},
		{ProjectID: input.ProjectID, AssistantID: input.AssistantID, RuntimeVersion: 3, Confirmed: true},
		{ProjectID: "", AssistantID: input.AssistantID, RuntimeVersion: 3, ExpectedRuntimeVersion: 2, Confirmed: true},
	} {
		_, err := service.selectRuntime(ctx, principal, invalid)
		require.Error(t, err)
	}
	_, err = service.selectRuntime(t.Context(), principal, input)
	require.Error(t, err)
	require.Equal(t, before, calls)
	var unavailable *AssistantIdentityService
	_, err = unavailable.selectRuntime(ctx, principal, input)
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestAssistantRuntimeRefusalIsBounded(t *testing.T) {
	t.Parallel()
	result, ok := assistantRuntimeToolResult(oops.E(oops.CodeConflict, nil, "private runtime metadata"))
	require.True(t, ok)
	require.True(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	require.Contains(t, text, "conflict")
	require.NotContains(t, text, "private")
}

func TestAssistantRuntimeToolContract(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerAssistantIdentityTool(live, &AssistantIdentityService{})
	registerAssistantIdentityTool(unavailable, nil)
	a := descriptorByName(t, live, selectAssistantRuntimeToolName)
	b := descriptorByName(t, unavailable, selectAssistantRuntimeToolName)
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, ExternalAuthorizationOrgAdmin, a.Meta.Authorization)
	require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
	require.Equal(t, externalOnly, a.Meta.Audiences)
	require.Empty(t, live.For(AudienceAssistant))
	for _, field := range []string{"project_id", "assistant_id", "runtime_version", "expected_runtime_version", "confirmed"} {
		require.Contains(t, string(a.InputSchema), field)
	}
	require.Contains(t, a.Description, "project:write")
	require.Contains(t, a.Description, "not authorization")
}
