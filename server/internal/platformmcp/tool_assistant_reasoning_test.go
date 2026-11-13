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

type assistantReasoningManagementStub struct {
	update func(context.Context, *genassistants.UpdateAssistantPayload) (*types.Assistant, error)
}

func (s assistantReasoningManagementStub) GetAssistant(ctx context.Context, p *genassistants.GetAssistantPayload) (*types.Assistant, error) {
	auth, _ := contextvalues.GetAuthContext(ctx)
	return &types.Assistant{ID: p.ID, ProjectID: auth.ProjectID.String(), ReasoningEffort: "medium"}, nil
}

func (s assistantReasoningManagementStub) UpdateAssistant(ctx context.Context, p *genassistants.UpdateAssistantPayload) (*types.Assistant, error) {
	return s.update(ctx, p)
}

func TestAssistantReasoningSelection(t *testing.T) {
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
		management: assistantReasoningManagementStub{update: func(ctx context.Context, p *genassistants.UpdateAssistantPayload) (*types.Assistant, error) {
			calls++
			scoped, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.Equal(t, projectID, *scoped.ProjectID)
			gotActor, ok := contextvalues.AuthenticatedActor(ctx)
			require.True(t, ok)
			require.Equal(t, actor, gotActor)
			require.Equal(t, assistantID.String(), p.ID)
			require.Equal(t, "high", *p.ReasoningEffort)
			require.Nil(t, p.RuntimeVersion)
			require.Nil(t, p.ExpectedRuntimeVersion)
			require.Nil(t, p.Name)
			require.Nil(t, p.Instructions)
			require.Nil(t, p.Toolsets)
			if endpointErr != nil {
				return nil, endpointErr
			}
			return &types.Assistant{ID: p.ID, ProjectID: projectID.String(), ReasoningEffort: "high", Instructions: "private"}, nil
		}},
	}
	input := SetAssistantReasoningEffortInput{ProjectID: projectID.String(), AssistantID: assistantID.String(), ReasoningEffort: "high", Confirmed: true}
	out, err := service.setReasoningEffort(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, "high", out.ReasoningEffort)
	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.Nil(t, auth.ProjectID)
	for _, code := range []oops.Code{oops.CodeConflict, oops.CodeForbidden} {
		endpointErr = oops.C(code)
		_, err = service.setReasoningEffort(ctx, principal, input)
		require.ErrorIs(t, err, endpointErr)
	}
	before := calls
	preview := input
	preview.Confirmed = false

	out, err = service.setReasoningEffort(ctx, principal, preview)
	require.NoError(t, err)
	require.Equal(t, "medium", out.ReasoningEffort)
	require.Equal(t, before, calls)

	for _, invalid := range []SetAssistantReasoningEffortInput{
		{ProjectID: input.ProjectID, AssistantID: input.AssistantID, ReasoningEffort: "invalid"},
		{ProjectID: input.ProjectID, AssistantID: input.AssistantID, ReasoningEffort: ""},
		{ProjectID: input.ProjectID, AssistantID: "invalid", ReasoningEffort: "high"},
		{ProjectID: "", AssistantID: input.AssistantID, ReasoningEffort: "high", Confirmed: true},
	} {
		_, err := service.setReasoningEffort(ctx, principal, invalid)
		require.Error(t, err)
	}
	_, err = service.setReasoningEffort(t.Context(), principal, input)
	require.Error(t, err)
	require.Equal(t, before, calls)
	var unavailable *AssistantIdentityService
	_, err = unavailable.setReasoningEffort(ctx, principal, input)
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestAssistantReasoningRefusalIsBounded(t *testing.T) {
	t.Parallel()
	result, ok := assistantReasoningEffortToolResult(oops.E(oops.CodeConflict, nil, "private runtime metadata"))
	require.True(t, ok)
	require.True(t, result.IsError)
	content, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	text := content.Text
	require.Contains(t, text, "conflict")
	require.NotContains(t, text, "private")
}

func TestAssistantReasoningToolContract(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerAssistantIdentityTool(live, &AssistantIdentityService{})
	registerAssistantIdentityTool(unavailable, nil)
	a := descriptorByName(t, live, setAssistantReasoningEffortToolName)
	b := descriptorByName(t, unavailable, setAssistantReasoningEffortToolName)
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, ExternalAuthorizationOrgAdmin, a.Meta.Authorization)
	require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
	require.Equal(t, externalOnly, a.Meta.Audiences)
	require.Empty(t, live.For(AudienceAssistant))
	for _, field := range []string{"project_id", "assistant_id", "reasoning_effort", "confirmed"} {
		require.Contains(t, string(a.InputSchema), field)
	}
	require.Contains(t, string(a.InputSchema), `"enum":["none","low","medium","high"]`)
	require.Contains(t, a.Description, "assistant:write")
	require.Contains(t, a.Description, "organization administrator")
	require.NotContains(t, a.Description, "project:write")
}

func TestAssistantReasoningPermissionDescription(t *testing.T) {
	t.Parallel()
	result, ok := assistantReasoningEffortToolResult(oops.C(oops.CodeForbidden))
	require.True(t, ok)
	require.True(t, result.IsError)
	content, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, content.Text, "assistant:write")
	require.Contains(t, content.Text, "organization administrator")
	require.NotContains(t, content.Text, "project:write")
}
