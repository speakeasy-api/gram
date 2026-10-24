package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	genassistants "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type assistantExecutionProfileManagementStub struct {
	assistantIdentityManagementFunc
	set func(context.Context, *genassistants.SetExecutionProfileChoicePayload) (*genassistants.AssistantExecutionProfile, error)
}

func (s assistantExecutionProfileManagementStub) ListExecutionProfiles(_ context.Context, p *genassistants.ListExecutionProfilesPayload) (*genassistants.ListExecutionProfilesResult, error) {
	return &genassistants.ListExecutionProfilesResult{Profiles: []*genassistants.AssistantExecutionProfile{{
		TriggerInstanceID: "trigger", TriggerName: "Dashboard", ExecutionContext: "userless", Version: 2,
		Servers: []*genassistants.ExecutionProfileServer{{McpServerID: "server", Chosen: false, GapReason: new("selection_required"), Options: []*genassistants.ExecutionProfileOption{{AuthClientID: "ac", Name: "Shared token", ClientType: "static_credential"}}}},
	}}}, nil
}

func (s assistantExecutionProfileManagementStub) SetExecutionProfileChoice(ctx context.Context, p *genassistants.SetExecutionProfileChoicePayload) (*genassistants.AssistantExecutionProfile, error) {
	return s.set(ctx, p)
}

func TestAssistantUpstreamCredentialChoice(t *testing.T) {
	t.Parallel()
	principal := Principal{OrganizationID: "test-org", UserID: "test-user"}
	projectID, assistantID, triggerID, serverID, clientID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	auth := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), auth, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	calls := 0
	var endpointErr error
	service := &AssistantIdentityService{
		organizations: riskMutationOrganizationResolver{slug: "test-org"},
		resolveProject: func(_ context.Context, _ string, in FindMCPInput) (ResolvedProject, error) {
			require.Equal(t, projectID.String(), in.ProjectID)
			return ResolvedProject{ID: projectID, Slug: "exact-project"}, nil
		},
		management: assistantExecutionProfileManagementStub{set: func(ctx context.Context, p *genassistants.SetExecutionProfileChoicePayload) (*genassistants.AssistantExecutionProfile, error) {
			calls++
			scoped, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.Equal(t, projectID, *scoped.ProjectID)
			require.Equal(t, clientID.String(), *p.AuthClientID)
			require.Nil(t, p.NoAuth)
			require.Nil(t, p.Automatic)
			require.Equal(t, int64(2), p.ExpectedVersion)
			if endpointErr != nil {
				return nil, endpointErr
			}
			return &genassistants.AssistantExecutionProfile{TriggerInstanceID: p.TriggerInstanceID, TriggerName: "Dashboard", ExecutionContext: p.ExecutionContext, Version: 3, Servers: []*genassistants.ExecutionProfileServer{{McpServerID: p.McpServerID, Chosen: true, AuthClientID: p.AuthClientID, Source: new("admin"), Options: []*genassistants.ExecutionProfileOption{}}}}, nil
		}},
	}

	preview, err := service.chooseUpstreamCredential(ctx, principal, ChooseAssistantUpstreamCredentialInput{ProjectID: projectID.String(), AssistantID: assistantID.String()})
	require.NoError(t, err)
	require.Len(t, preview.Profiles, 1)
	require.Equal(t, "selection_required", preview.Profiles[0].Servers[0].GapReason)
	require.Equal(t, 0, calls)

	input := ChooseAssistantUpstreamCredentialInput{
		ProjectID: projectID.String(), AssistantID: assistantID.String(), TriggerInstanceID: triggerID.String(), ExecutionContext: "userless",
		McpServerID: serverID.String(), AuthClientID: clientID.String(), ExpectedVersion: 2, Confirmed: true,
	}
	out, err := service.chooseUpstreamCredential(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, int64(3), out.Profiles[0].Version)
	require.Equal(t, clientID.String(), out.Profiles[0].Servers[0].AuthClientID)
	require.Nil(t, auth.ProjectID, "the caller's context is not changed")

	for _, code := range []oops.Code{oops.CodeConflict, oops.CodeForbidden} {
		endpointErr = oops.C(code)
		_, err = service.chooseUpstreamCredential(ctx, principal, input)
		require.ErrorIs(t, err, endpointErr)
	}

	before := calls
	for _, invalid := range []func(*ChooseAssistantUpstreamCredentialInput){
		func(in *ChooseAssistantUpstreamCredentialInput) { in.NoAuth = true },
		func(in *ChooseAssistantUpstreamCredentialInput) { in.AuthClientID = "" },
		func(in *ChooseAssistantUpstreamCredentialInput) { in.ExecutionContext = "owner" },
		func(in *ChooseAssistantUpstreamCredentialInput) { in.TriggerInstanceID = "invalid" },
		func(in *ChooseAssistantUpstreamCredentialInput) { in.ProjectID = "" },
	} {
		bad := input
		invalid(&bad)
		_, err := service.chooseUpstreamCredential(ctx, principal, bad)
		require.Error(t, err)
	}
	_, err = service.chooseUpstreamCredential(t.Context(), principal, input)
	require.Error(t, err)
	require.Equal(t, before, calls)
	var unavailable *AssistantIdentityService
	_, err = unavailable.chooseUpstreamCredential(ctx, principal, input)
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestAssistantUpstreamCredentialRefusalIsBounded(t *testing.T) {
	t.Parallel()
	result, ok := assistantUpstreamCredentialToolResult(oops.E(oops.CodeConflict, nil, "private profile metadata"))
	require.True(t, ok)
	require.True(t, result.IsError)
	content, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, content.Text, "conflict")
	require.NotContains(t, content.Text, "private")
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(content.Text), &decoded))
}

func TestAssistantUpstreamCredentialToolContract(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerAssistantIdentityTool(live, &AssistantIdentityService{})
	registerAssistantIdentityTool(unavailable, nil)
	a := descriptorByName(t, live, chooseAssistantUpstreamCredentialToolName)
	b := descriptorByName(t, unavailable, chooseAssistantUpstreamCredentialToolName)
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, ExternalAuthorizationOrgAdmin, a.Meta.Authorization)
	require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
	require.Equal(t, externalOnly, a.Meta.Audiences)
	for _, field := range []string{"project_id", "assistant_id", "trigger_instance_id", "execution_context", "mcp_server_id", "auth_client_id", "expected_version", "confirmed"} {
		require.Contains(t, string(a.InputSchema), field)
	}
	require.Contains(t, a.Description, "assistant:write")
}
