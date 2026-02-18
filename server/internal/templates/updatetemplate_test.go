package templates_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/templates"
	"github.com/speakeasy-api/gram/server/gen/types"
)

func TestTemplatesService_UpdateTemplate_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("update-test-template"),
		Prompt:           "Original prompt",
		Description:      new("Original description"),
		Engine:           "mustache",
		Kind:             "prompt",
		ToolsHint:        []string{"system"},
		Arguments:        new(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "create template")

	// Update the template
	result, err := ti.service.UpdateTemplate(ctx, &gen.UpdateTemplatePayload{
		ID:               created.Template.ID,
		Prompt:           new("Updated prompt {{name}}!"),
		Description:      new("Updated description"),
		Engine:           new("mustache"),
		Kind:             new("prompt"),
		ToolsHint:        []string{"user", "assistant"},
		Arguments:        new(`{"type": "object", "properties": {"message": {"type": "string"}}, "required": ["message"]}`),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "update template")

	require.NotNil(t, result, "result is nil")
	require.NotNil(t, result.Template, "template is nil")
	require.Equal(t, "update-test-template", result.Template.Name, "template name should not change")
	require.Equal(t, "Updated prompt {{name}}!", result.Template.Prompt, "template prompt mismatch")
	require.Equal(t, "Updated description", result.Template.Description, "template description mismatch")
	require.Equal(t, "mustache", result.Template.Engine, "template engine should remain unchanged when updating to empty")
	require.Equal(t, "prompt", result.Template.Kind, "template kind mismatch")
	require.ElementsMatch(t, []string{"user", "assistant"}, result.Template.ToolsHint, "template tools hint mismatch")
	require.JSONEq(t, `{"type": "object", "properties": {"message": {"type": "string"}}, "required": ["message"]}`, result.Template.Schema, "template arguments mismatch")
	require.Equal(t, created.Template.HistoryID, result.Template.HistoryID, "history id should remain the same (same logical template)")
	// Note: CreatedAt and UpdatedAt may change when updates create a new version (append-only versioning)

	// Render the updated template to ensure the update version is used by the server
	rendered, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID: result.Template.ID,
		Arguments: map[string]any{
			"name": "TestUser",
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render updated template")
	require.NotNil(t, rendered, "rendered result is nil")
	require.Equal(t, "Updated prompt TestUser!", rendered.Prompt, "rendered prompt should use updated template")
}

func TestTemplatesService_UpdateTemplate_PartialUpdate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("partial-update-template"),
		Prompt:           "Original prompt",
		Description:      new("Original description"),
		Engine:           "mustache",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Arguments:        nil,
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Update only the prompt
	result, err := ti.service.UpdateTemplate(ctx, &gen.UpdateTemplatePayload{
		ID:               created.Template.ID,
		Prompt:           new("Only updated prompt"),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Engine:           nil,
		Kind:             nil,
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "update template")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Only updated prompt", result.Template.Prompt, "template prompt mismatch")
	require.Equal(t, "Original description", result.Template.Description, "description should remain unchanged")
	require.Equal(t, "mustache", result.Template.Engine, "engine should remain unchanged")
}

func TestTemplatesService_UpdateTemplate_NoChanges(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("no-changes-template"),
		Prompt:           "Original prompt",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Update with no actual changes (empty payload except ID)
	result, err := ti.service.UpdateTemplate(ctx, &gen.UpdateTemplatePayload{
		ID:               created.Template.ID,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Prompt:           nil,
		Description:      nil,
		Arguments:        nil,
		Engine:           nil,
		Kind:             nil,
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "update template with no changes")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, created.Template.ID, result.Template.ID, "template ID should remain the same")
	require.Equal(t, "Original prompt", result.Template.Prompt, "prompt should remain unchanged")
}

func TestTemplatesService_UpdateTemplateName_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("update-name-template"),
		Prompt:           "Original prompt",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Update the template name
	result, err := ti.service.UpdateTemplate(ctx, &gen.UpdateTemplatePayload{
		ID:   created.Template.ID,
		Name: new("New name"),
	})
	require.NoError(t, err, "update template name")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "New name", result.Template.Name, "template name mismatch")
}

func TestTemplatesService_UpdateTemplate_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to update with invalid UUID
	_, err := ti.service.UpdateTemplate(ctx, &gen.UpdateTemplatePayload{
		ID:               "invalid-uuid",
		Prompt:           new("Updated prompt"),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Engine:           nil,
		Kind:             nil,
		ToolsHint:        []string{},
	})
	require.Error(t, err, "expected error for invalid UUID")
	require.Contains(t, err.Error(), "invalid template id")
}

func TestTemplatesService_UpdateTemplate_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to update with non-existent UUID
	nonExistentID := uuid.New().String()
	_, err := ti.service.UpdateTemplate(ctx, &gen.UpdateTemplatePayload{
		ID:               nonExistentID,
		Prompt:           new("Updated prompt"),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Engine:           nil,
		Kind:             nil,
		ToolsHint:        []string{},
	})
	require.Error(t, err, "expected error for non-existent template")
	require.Contains(t, err.Error(), "template not found")
}

func TestTemplatesService_UpdateTemplate_InvalidArguments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("invalid-args-update-template"),
		Prompt:           "Original prompt",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	tests := []struct {
		name      string
		arguments string
		errorMsg  string
	}{
		{
			name:      "invalid json",
			arguments: `{"type": "object", "properties": {"name": {"type": "string"}`,
			errorMsg:  "failed to validate arguments schema",
		},
		{
			name:      "invalid schema - non-object type",
			arguments: `{"type": "string"}`,
			errorMsg:  "invalid arguments schema",
		},
		{
			name:      "invalid schema - unsupported type",
			arguments: `{"type": "array"}`,
			errorMsg:  "invalid arguments schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ti.service.UpdateTemplate(ctx, &gen.UpdateTemplatePayload{
				ID:               created.Template.ID,
				Arguments:        &tt.arguments,
				ApikeyToken:      nil,
				SessionToken:     nil,
				ProjectSlugInput: nil,
				Prompt:           nil,
				Description:      nil,
				Engine:           nil,
				Kind:             nil,
				ToolsHint:        []string{},
			})
			require.Error(t, err, "expected error for %s", tt.name)
			require.Contains(t, err.Error(), tt.errorMsg, "error message should contain: %s", tt.errorMsg)
		})
	}
}

func TestTemplatesService_UpdateTemplate_ArgumentsWithoutEngine(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template without an engine
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("update-args-no-engine"),
		Prompt:           "Original prompt",
		Description:      nil,
		Engine:           "", // No engine
		Kind:             "prompt",
		ToolsHint:        nil,
		Arguments:        nil, // No arguments initially
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "create template without engine")

	// Try to update with arguments but no engine
	result, err := ti.service.UpdateTemplate(ctx, &gen.UpdateTemplatePayload{
		ID:               created.Template.ID,
		Prompt:           nil,
		Description:      nil,
		Engine:           nil, // Not setting engine
		Kind:             nil,
		ToolsHint:        nil,
		Arguments:        new(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "updating template with arguments but no engine should succeed")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Template, "template should not be nil")
}

func TestTemplatesService_UpdateTemplate_ArgumentsWithExistingEngine(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template with an engine
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("update-args-with-engine"),
		Prompt:           "Original prompt",
		Description:      nil,
		Engine:           "mustache", // Has engine
		Kind:             "prompt",
		ToolsHint:        nil,
		Arguments:        nil, // No arguments initially
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "create template with engine")

	// Update with arguments should succeed since engine exists
	result, err := ti.service.UpdateTemplate(ctx, &gen.UpdateTemplatePayload{
		ID:               created.Template.ID,
		Prompt:           nil,
		Description:      nil,
		Engine:           nil, // Not changing engine
		Kind:             nil,
		ToolsHint:        nil,
		Arguments:        new(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "should succeed when updating with arguments and existing engine")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Template, "template should not be nil")
	require.Equal(t, "mustache", result.Template.Engine, "engine should remain mustache")
	// Note: Arguments might be nil if no actual update was needed, which is okay for validation purposes
}

func TestTemplatesService_UpdateTemplate_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestTemplateService(t)

	// Create context without auth
	ctx := t.Context()
	templateID := uuid.New().String()

	_, err := ti.service.UpdateTemplate(ctx, &gen.UpdateTemplatePayload{
		ID:               templateID,
		Prompt:           new("Updated prompt"),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Engine:           nil,
		Kind:             nil,
		ToolsHint:        []string{},
	})
	require.Error(t, err, "expected error for unauthorized request")
	require.Contains(t, err.Error(), "unauthorized")
}
