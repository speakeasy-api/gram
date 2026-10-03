package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
)

func chatMetadataRegistrar(t *testing.T, reader chatMetadataReader) (*Registrar, *ChatMetadataService) {
	t.Helper()

	service, _ := newChatMetadataService(t, reader, allowBudget())
	registrar := newRegistrar(newTestMCPServer())
	registerChatMetadataTools(registrar, service)
	return registrar, service
}

func TestListChatsToolDeclaresBothAudiencesAndAdminAuthorization(t *testing.T) {
	t.Parallel()

	registrar, _ := chatMetadataRegistrar(t, &recordingChatReader{rows: nil, params: nil, err: nil, countParams: nil, count: 0})
	descriptor := descriptorByName(t, registrar, listChatsToolName)
	require.Equal(t, bothAudiences, descriptor.Meta.Audiences)
	require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
	require.Equal(t, ProjectScopeDefaultable, descriptor.Meta.ProjectScope)
	require.Contains(t, descriptor.Description, "metadata only")
	require.Contains(t, descriptor.Description, "no titles, messages, prompts, tool inputs or outputs, or raw identities")

	// The schema is closed and carries the project selectors the assistant
	// policy injects, so the model is never asked to choose a project.
	var schema map[string]any
	require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	require.ElementsMatch(t, []string{"project_id", "project_slug", "window", "risk", "source", "assistant_id", "user_reference", "limit", "cursor"}, keysOf(properties))
}

func TestListChatsToolServesAPage(t *testing.T) {
	t.Parallel()

	reader := &recordingChatReader{rows: []chatrepo.ListChatsRow{chatListRow(uuid.New(), chatListTestNow, "user_1", "", "", 1)}, params: nil, err: nil, countParams: nil, count: 0}
	registrar, _ := chatMetadataRegistrar(t, reader)

	result, err := descriptorByName(t, registrar, listChatsToolName).Invoke(
		ContextWithPrincipal(t.Context(), registrationServicePrincipal()),
		json.RawMessage(`{"window":"24h","risk":"with_findings","limit":10}`),
	)
	require.NoError(t, err)
	output, ok := result.(ListChatsOutput)
	require.True(t, ok)
	require.Len(t, output.Chats, 1)
	require.Equal(t, "u***", output.Chats[0].MaskedIdentity)
	require.Len(t, reader.params, 1)
	require.Equal(t, "true", reader.params[0].HasRiskFilter)
}

func TestListChatsToolRefusesOutsideTheSchema(t *testing.T) {
	t.Parallel()

	reader := &recordingChatReader{rows: nil, params: nil, err: nil, countParams: nil, count: 0}
	registrar, _ := chatMetadataRegistrar(t, reader)
	ctx := ContextWithPrincipal(t.Context(), registrationServicePrincipal())

	for name, arguments := range map[string]string{
		"unknown window":   `{"window":"90d"}`,
		"unknown risk":     `{"risk":"maybe"}`,
		"limit over cap":   `{"limit":51}`,
		"limit under one":  `{"limit":0}`,
		"unknown property": `{"search":"password"}`,
		"both selectors":   `{"project_id":"` + uuid.NewString() + `","project_slug":"default"}`,
	} {
		_, err := descriptorByName(t, registrar, listChatsToolName).Invoke(ctx, json.RawMessage(arguments))
		require.ErrorContains(t, err, "arguments do not match the tool schema", name)
	}
	require.Empty(t, reader.params, "schema refusals never reach the handler")
}

func TestListChatsToolMapsRefusals(t *testing.T) {
	t.Parallel()

	reader := &recordingChatReader{rows: nil, params: nil, err: nil, countParams: nil, count: 0}
	registrar, service := chatMetadataRegistrar(t, reader)
	ctx := ContextWithPrincipal(t.Context(), registrationServicePrincipal())
	descriptor := descriptorByName(t, registrar, listChatsToolName)

	_, err := descriptor.Invoke(ctx, json.RawMessage(`{"cursor":"stale"}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.JSONEq(t, `{"code":"not_found","message":"That cursor or person reference is not available here. List the chats again with the same filters and use a value from the new result."}`, refusal.Payload)

	_, err = descriptor.Invoke(ctx, json.RawMessage(`{"user_reference":"stale"}`))
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"not_found"`)

	projects, ok := service.projects.(*findingProjects)
	require.True(t, ok)
	projects.err = ErrRiskReadNotFound
	_, err = descriptor.Invoke(ctx, json.RawMessage(`{"project_id":"`+uuid.NewString()+`"}`))
	require.ErrorAs(t, err, &refusal)
	require.JSONEq(t, `{"code":"not_found","message":"That project is not one this organization holds. Choose one returned by list_projects."}`, refusal.Payload)
	projects.err = nil

	service.budget = OperationBudget{Connection: denyOperationLimiter{}, Organization: allowOperationLimiter{}}
	_, err = descriptor.Invoke(ctx, json.RawMessage(`{}`))
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"rate_limited"`)
	require.Empty(t, reader.params)
}

func TestListChatsToolRequiresPrincipal(t *testing.T) {
	t.Parallel()

	registrar, _ := chatMetadataRegistrar(t, &recordingChatReader{rows: nil, params: nil, err: nil, countParams: nil, count: 0})
	_, err := descriptorByName(t, registrar, listChatsToolName).Invoke(t.Context(), json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestListChatsIsServedOnBothSurfaces(t *testing.T) {
	t.Parallel()

	_, registrar := newTestServer(t)
	descriptor := descriptorByName(t, registrar, listChatsToolName)
	require.Equal(t, bothAudiences, descriptor.Meta.Audiences)
}
