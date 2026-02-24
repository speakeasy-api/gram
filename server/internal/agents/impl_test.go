package agents_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateAgentDefinition(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	result, err := ti.service.CreateAgentDefinition(ctx, &gen.CreateAgentDefinitionPayload{
		Name:         "test-agent",
		Description:  "A test agent",
		Instructions: "Do test things",
		Tools:        nil,
	})
	require.NoError(t, err)

	require.NotEmpty(t, result.ID)
	require.NotEmpty(t, result.ProjectID)
	require.NotEmpty(t, result.ToolUrn)
	require.Equal(t, "test-agent", result.Name)
	require.Equal(t, "A test agent", result.Description)
	require.Equal(t, "Do test things", result.Instructions)
	require.Empty(t, result.Tools)
	require.NotEmpty(t, result.CreatedAt)
	require.NotEmpty(t, result.UpdatedAt)
	require.Contains(t, result.ToolUrn, "agent")
}

func TestCreateAgentDefinitionWithOptionalFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	title := "My Agent Title"
	model := "claude-sonnet-4-20250514"
	readOnly := true

	result, err := ti.service.CreateAgentDefinition(ctx, &gen.CreateAgentDefinitionPayload{
		Name:         "full-agent",
		Description:  "A fully specified agent",
		Instructions: "Do all the things",
		Title:        &title,
		Model:        &model,
		ReadOnlyHint: &readOnly,
		Tools:        nil,
	})
	require.NoError(t, err)

	require.Equal(t, &title, result.Title)
	require.Equal(t, &model, result.Model)
	require.NotNil(t, result.Annotations)
	require.Equal(t, &readOnly, result.Annotations.ReadOnlyHint)
}

func TestCreateAgentDefinitionConflict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	payload := &gen.CreateAgentDefinitionPayload{
		Name:         "duplicate-agent",
		Description:  "First agent",
		Instructions: "Do things",
		Tools:        nil,
	}

	_, err := ti.service.CreateAgentDefinition(ctx, payload)
	require.NoError(t, err)

	_, err = ti.service.CreateAgentDefinition(ctx, payload)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestCreateAgentDefinitionUnauthorized(t *testing.T) {
	t.Parallel()
	_, ti := newTestAgentsService(t)

	ctxWithoutAuth := t.Context()

	_, err := ti.service.CreateAgentDefinition(ctxWithoutAuth, &gen.CreateAgentDefinitionPayload{
		Name:         "unauthorized-agent",
		Description:  "Should fail",
		Instructions: "Do things",
		Tools:        nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestGetAgentDefinition(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	created, err := ti.service.CreateAgentDefinition(ctx, &gen.CreateAgentDefinitionPayload{
		Name:         "get-agent",
		Description:  "An agent to get",
		Instructions: "Do get things",
		Tools:        nil,
	})
	require.NoError(t, err)

	result, err := ti.service.GetAgentDefinition(ctx, &gen.GetAgentDefinitionPayload{
		ID: created.ID,
	})
	require.NoError(t, err)

	require.Equal(t, created.ID, result.ID)
	require.Equal(t, created.Name, result.Name)
	require.Equal(t, created.Description, result.Description)
	require.Equal(t, created.Instructions, result.Instructions)
	require.Equal(t, created.ToolUrn, result.ToolUrn)
}

func TestGetAgentDefinitionNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	_, err := ti.service.GetAgentDefinition(ctx, &gen.GetAgentDefinitionPayload{
		ID: uuid.New().String(),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestGetAgentDefinitionUnauthorized(t *testing.T) {
	t.Parallel()
	_, ti := newTestAgentsService(t)

	ctxWithoutAuth := t.Context()

	_, err := ti.service.GetAgentDefinition(ctxWithoutAuth, &gen.GetAgentDefinitionPayload{
		ID: uuid.New().String(),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestListAgentDefinitionsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	result, err := ti.service.ListAgentDefinitions(ctx, &gen.ListAgentDefinitionsPayload{})
	require.NoError(t, err)
	require.Empty(t, result.AgentDefinitions)
}

func TestListAgentDefinitionsPopulated(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	_, err := ti.service.CreateAgentDefinition(ctx, &gen.CreateAgentDefinitionPayload{
		Name:         "list-agent-1",
		Description:  "First agent",
		Instructions: "Do first things",
		Tools:        nil,
	})
	require.NoError(t, err)

	_, err = ti.service.CreateAgentDefinition(ctx, &gen.CreateAgentDefinitionPayload{
		Name:         "list-agent-2",
		Description:  "Second agent",
		Instructions: "Do second things",
		Tools:        nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListAgentDefinitions(ctx, &gen.ListAgentDefinitionsPayload{})
	require.NoError(t, err)
	require.Len(t, result.AgentDefinitions, 2)
}

func TestListAgentDefinitionsUnauthorized(t *testing.T) {
	t.Parallel()
	_, ti := newTestAgentsService(t)

	ctxWithoutAuth := t.Context()

	_, err := ti.service.ListAgentDefinitions(ctxWithoutAuth, &gen.ListAgentDefinitionsPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestUpdateAgentDefinition(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	created, err := ti.service.CreateAgentDefinition(ctx, &gen.CreateAgentDefinitionPayload{
		Name:         "update-agent",
		Description:  "Original description",
		Instructions: "Original instructions",
		Tools:        nil,
	})
	require.NoError(t, err)

	newDesc := "Updated description"
	newInstructions := "Updated instructions"
	newTitle := "Updated Title"

	result, err := ti.service.UpdateAgentDefinition(ctx, &gen.UpdateAgentDefinitionPayload{
		ID:           created.ID,
		Description:  &newDesc,
		Instructions: &newInstructions,
		Title:        &newTitle,
	})
	require.NoError(t, err)

	require.Equal(t, created.ID, result.ID)
	require.Equal(t, "update-agent", result.Name)
	require.Equal(t, "Updated description", result.Description)
	require.Equal(t, "Updated instructions", result.Instructions)
	require.Equal(t, &newTitle, result.Title)
}

func TestUpdateAgentDefinitionPartialFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	created, err := ti.service.CreateAgentDefinition(ctx, &gen.CreateAgentDefinitionPayload{
		Name:         "partial-update-agent",
		Description:  "Original description",
		Instructions: "Original instructions",
		Tools:        nil,
	})
	require.NoError(t, err)

	newDesc := "Only description updated"

	result, err := ti.service.UpdateAgentDefinition(ctx, &gen.UpdateAgentDefinitionPayload{
		ID:          created.ID,
		Description: &newDesc,
	})
	require.NoError(t, err)

	require.Equal(t, "Only description updated", result.Description)
	require.Equal(t, "Original instructions", result.Instructions)
}

func TestUpdateAgentDefinitionNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	newDesc := "Updated description"
	_, err := ti.service.UpdateAgentDefinition(ctx, &gen.UpdateAgentDefinitionPayload{
		ID:          uuid.New().String(),
		Description: &newDesc,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestUpdateAgentDefinitionUnauthorized(t *testing.T) {
	t.Parallel()
	_, ti := newTestAgentsService(t)

	ctxWithoutAuth := t.Context()
	newDesc := "Updated description"

	_, err := ti.service.UpdateAgentDefinition(ctxWithoutAuth, &gen.UpdateAgentDefinitionPayload{
		ID:          uuid.New().String(),
		Description: &newDesc,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestDeleteAgentDefinition(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	created, err := ti.service.CreateAgentDefinition(ctx, &gen.CreateAgentDefinitionPayload{
		Name:         "delete-agent",
		Description:  "An agent to delete",
		Instructions: "Do delete things",
		Tools:        nil,
	})
	require.NoError(t, err)

	err = ti.service.DeleteAgentDefinition(ctx, &gen.DeleteAgentDefinitionPayload{
		ID: created.ID,
	})
	require.NoError(t, err)

	_, err = ti.service.GetAgentDefinition(ctx, &gen.GetAgentDefinitionPayload{
		ID: created.ID,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestDeleteAgentDefinitionIdempotent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	created, err := ti.service.CreateAgentDefinition(ctx, &gen.CreateAgentDefinitionPayload{
		Name:         "idem-delete-agent",
		Description:  "An agent to delete twice",
		Instructions: "Do delete things",
		Tools:        nil,
	})
	require.NoError(t, err)

	err = ti.service.DeleteAgentDefinition(ctx, &gen.DeleteAgentDefinitionPayload{
		ID: created.ID,
	})
	require.NoError(t, err)

	err = ti.service.DeleteAgentDefinition(ctx, &gen.DeleteAgentDefinitionPayload{
		ID: created.ID,
	})
	require.NoError(t, err)
}

func TestDeleteAgentDefinitionUnauthorized(t *testing.T) {
	t.Parallel()
	_, ti := newTestAgentsService(t)

	ctxWithoutAuth := t.Context()

	err := ti.service.DeleteAgentDefinition(ctxWithoutAuth, &gen.DeleteAgentDefinitionPayload{
		ID: uuid.New().String(),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestDeletedAgentNotInList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	created, err := ti.service.CreateAgentDefinition(ctx, &gen.CreateAgentDefinitionPayload{
		Name:         "list-delete-agent",
		Description:  "An agent to be deleted from list",
		Instructions: "Do things",
		Tools:        nil,
	})
	require.NoError(t, err)

	err = ti.service.DeleteAgentDefinition(ctx, &gen.DeleteAgentDefinitionPayload{
		ID: created.ID,
	})
	require.NoError(t, err)

	result, err := ti.service.ListAgentDefinitions(ctx, &gen.ListAgentDefinitionsPayload{})
	require.NoError(t, err)

	for _, agent := range result.AgentDefinitions {
		require.NotEqual(t, created.ID, agent.ID)
	}
}

func TestCreateAgentDefinitionNoProjectContext(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	authCtx.ProjectID = nil
	ctxWithoutProject := contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.CreateAgentDefinition(ctxWithoutProject, &gen.CreateAgentDefinitionPayload{
		Name:         "no-project-agent",
		Description:  "Should fail",
		Instructions: "Do things",
		Tools:        nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
