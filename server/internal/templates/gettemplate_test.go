package templates_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/templates"
	"github.com/speakeasy-api/gram/server/gen/types"
)

func TestTemplatesService_GetTemplate_ByID_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("get-by-id-template"),
		Prompt:           "Test prompt for get by ID",
		Description:      new("Test description"),
		Engine:           "mustache",
		Kind:             "prompt",
		ToolsHint:        []string{"assistant"},
		Arguments:        new(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`),
	})
	require.NoError(t, err, "create template")

	// Get the template by ID
	result, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new(created.Template.ID),
		Name:             nil,
	})
	require.NoError(t, err, "get template by ID")

	require.NotNil(t, result, "result is nil")
	require.NotNil(t, result.Template, "template is nil")
	require.Equal(t, created.Template.ID, result.Template.ID, "template ID mismatch")
	require.Equal(t, "get-by-id-template", result.Template.Name, "template name mismatch")
	require.Equal(t, "Test prompt for get by ID", result.Template.Prompt, "template prompt mismatch")
	require.Equal(t, "Test description", result.Template.Description, "template description mismatch")
	require.Equal(t, "mustache", result.Template.Engine, "template engine mismatch")
	require.Equal(t, "prompt", result.Template.Kind, "template kind mismatch")
	require.ElementsMatch(t, []string{"assistant"}, result.Template.ToolsHint, "template tools hint mismatch")
	require.JSONEq(t, `{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`, result.Template.Schema, "template arguments mismatch")
}

func TestTemplatesService_GetTemplate_ByName_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("get-by-name-template"),
		Prompt:           "Test prompt for get by name",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
	})
	require.NoError(t, err, "create template")

	// Get the template by name
	result, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               nil,
		Name:             new("get-by-name-template"),
	})
	require.NoError(t, err, "get template by name")

	require.NotNil(t, result, "result is nil")
	require.NotNil(t, result.Template, "template is nil")
	require.Equal(t, created.Template.ID, result.Template.ID, "template ID mismatch")
	require.Equal(t, "get-by-name-template", result.Template.Name, "template name mismatch")
	require.Equal(t, "Test prompt for get by name", result.Template.Prompt, "template prompt mismatch")
}

func TestTemplatesService_GetTemplate_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to get with invalid UUID
	_, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new("invalid-uuid"),
		Name:             nil,
	})
	require.Error(t, err, "expected error for invalid UUID")
	require.Contains(t, err.Error(), "invalid template id")
}

func TestTemplatesService_GetTemplate_NonExistentID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to get non-existent template by ID
	nonExistentID := uuid.New().String()
	_, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               &nonExistentID,
		Name:             nil,
	})
	require.Error(t, err, "expected error for non-existent template")
	require.Contains(t, err.Error(), "not found")
}

func TestTemplatesService_GetTemplate_NonExistentName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to get non-existent template by name
	_, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               nil,
		Name:             new("non-existent-template"),
	})
	require.Error(t, err, "expected error for non-existent template")
	require.Contains(t, err.Error(), "not found")
}

func TestTemplatesService_GetTemplate_EmptyIDAndName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to get without providing ID or name
	_, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               nil,
		Name:             nil,
	})
	require.Error(t, err, "expected error when neither ID nor name provided")
	require.Contains(t, err.Error(), "either id or name must be provided")
}

func TestTemplatesService_GetTemplate_EmptyID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to get with empty ID
	_, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new(""),
		Name:             nil,
	})
	require.Error(t, err, "expected error for empty ID")
	require.Contains(t, err.Error(), "either id or name must be provided")
}

func TestTemplatesService_GetTemplate_EmptyName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to get with empty name
	_, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               nil,
		Name:             new(""),
	})
	require.Error(t, err, "expected error for empty name")
	require.Contains(t, err.Error(), "either id or name must be provided")
}

func TestTemplatesService_GetTemplate_NilUUID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to get with nil UUID value
	_, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new(uuid.Nil.String()),
		Name:             nil,
	})
	require.Error(t, err, "expected error when neither ID nor name provided")
	require.Contains(t, err.Error(), "either id or name must be provided")
}

func TestTemplatesService_GetTemplate_BothIDAndName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("both-id-name-template"),
		Prompt:           "Test prompt for both ID and name",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
	})
	require.NoError(t, err, "create template")

	// Get the template providing both ID and name (ID should take precedence)
	result, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new(created.Template.ID),
		Name:             new("both-id-name-template"),
	})
	require.NoError(t, err, "get template with both ID and name")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, created.Template.ID, result.Template.ID, "template ID mismatch")
	require.Equal(t, "both-id-name-template", result.Template.Name, "template name mismatch")
}

func TestTemplatesService_GetTemplate_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestTemplateService(t)

	// Create context without auth
	ctx := t.Context()
	templateID := uuid.New().String()

	_, err := ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               &templateID,
		Name:             nil,
	})
	require.Error(t, err, "expected error for unauthorized request")
	require.Contains(t, err.Error(), "unauthorized")
}
