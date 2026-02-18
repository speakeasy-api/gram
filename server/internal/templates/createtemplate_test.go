package templates_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/templates"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

func TestTemplatesService_CreateTemplate_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	result, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("test-template"),
		Prompt:           "Hello {{name}}!",
		Description:      new("A test template"),
		Engine:           "mustache",
		Kind:             "prompt",
		ToolsHint:        []string{"assistant"},
		Arguments:        new(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`),
	})
	require.NoError(t, err, "create template")

	require.NotNil(t, result, "result is nil")
	require.NotNil(t, result.Template, "template is nil")
	require.NotEqual(t, uuid.Nil.String(), result.Template.ID, "template ID is nil")
	require.Equal(t, "test-template", result.Template.Name, "template name mismatch")
	require.Equal(t, "Hello {{name}}!", result.Template.Prompt, "template prompt mismatch")
	require.Equal(t, "A test template", result.Template.Description, "template description mismatch")
	require.Equal(t, "mustache", result.Template.Engine, "template engine mismatch")
	require.Equal(t, "prompt", result.Template.Kind, "template kind mismatch")
	require.ElementsMatch(t, []string{"assistant"}, result.Template.ToolsHint, "template tools hint mismatch")
	require.JSONEq(t, `{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`, result.Template.Schema, "template arguments mismatch")
	require.NotNil(t, result.Template.CreatedAt, "template created at is nil")
	require.NotNil(t, result.Template.UpdatedAt, "template updated at is nil")
}

func TestTemplatesService_CreateTemplate_MinimalPayload(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	result, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("minimal-template"),
		Prompt:           "Simple prompt",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
	})
	require.NoError(t, err, "create template")

	require.NotNil(t, result, "result is nil")
	require.NotNil(t, result.Template, "template is nil")
	require.Equal(t, "minimal-template", result.Template.Name, "template name mismatch")
	require.Equal(t, "Simple prompt", result.Template.Prompt, "template prompt mismatch")
	require.Empty(t, result.Template.Description, "template description should be empty")
	require.Empty(t, result.Template.Engine, "template engine should be empty")
	require.Equal(t, "prompt", result.Template.Kind, "template kind should default to prompt")
	require.Empty(t, result.Template.ToolsHint, "template tools hint should be empty")
	require.JSONEq(t, constants.DefaultEmptyToolSchema, result.Template.Schema, "template arguments should be empty object")
}

func TestTemplatesService_CreateTemplate_DuplicateName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create first template
	_, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("duplicate-template"),
		Prompt:           "First template",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
	})
	require.NoError(t, err, "create first template")

	// Try to create second template with same name
	_, err = ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("duplicate-template"),
		Prompt:           "Second template",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
	})
	require.Error(t, err, "expected error for duplicate name")
	require.Contains(t, err.Error(), "template name already exists")
}

func TestTemplatesService_CreateTemplate_InvalidArguments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	tests := []struct {
		name      string
		arguments string
		errorMsg  string
	}{
		{
			name:      "invalid-json",
			arguments: `{"type": "object", "properties": {"name": {"type": "string"}`,
			errorMsg:  "failed to validate arguments schema",
		},
		{
			name:      "non-object-type",
			arguments: `{"type": "string"}`,
			errorMsg:  "invalid arguments schema",
		},
		{
			name:      "unsupported-type",
			arguments: `{"type": "array"}`,
			errorMsg:  "invalid arguments schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
				ApikeyToken:      nil,
				SessionToken:     nil,
				ProjectSlugInput: nil,
				Name:             types.Slug("test-" + tt.name),
				Prompt:           "Test prompt",
				Description:      nil,
				Engine:           "",
				Kind:             "prompt",
				ToolsHint:        nil,
				Arguments:        &tt.arguments,
			})
			require.Error(t, err, "expected error for %s", tt.name)
			require.Contains(t, err.Error(), tt.errorMsg, "error message should contain: %s", tt.errorMsg)
		})
	}
}

func TestTemplatesService_CreateTemplate_EmptyArgumentsSchema(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Schema with no properties should be allowed but trigger warning
	result, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("empty-args-template"),
		Prompt:           "Test prompt",
		Description:      nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
		Arguments:        new(`{"type": "object"}`),
	})
	require.NoError(t, err, "create template with empty arguments schema should succeed")
	require.NotNil(t, result, "result is nil")
	require.Equal(t, "empty-args-template", result.Template.Name, "template name mismatch")
}

func TestTemplatesService_CreateTemplate_ArgumentsWithoutEngine(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	result, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("args-no-engine"),
		Prompt:           "Test prompt",
		Description:      nil,
		Engine:           "", // No engine specified
		Kind:             "prompt",
		ToolsHint:        nil,
		Arguments:        new(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`),
	})
	require.NoError(t, err, "creating template with arguments but no engine should succeed")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Template, "template should not be nil")
}

func TestTemplatesService_CreateTemplate_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestTemplateService(t)

	// Create context without auth
	ctx := t.Context()

	_, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("unauthorized-template"),
		Prompt:           "Test prompt",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.Error(t, err, "expected error for unauthorized request")
	require.Contains(t, err.Error(), "unauthorized")
}
