package templates_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/gen/templates"
	"github.com/speakeasy-api/gram/gen/types"
	"github.com/speakeasy-api/gram/internal/conv"
)

func TestTemplatesService_RenderTemplate_NoEngine_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a template without an engine (plain text)
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("no-engine-template"),
		Prompt:           "Hello, this is a plain text template",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template
	result, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		ID:               created.Template.ID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello, this is a plain text template", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplate_EmptyEngine_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a template with empty engine (plain text)
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("empty-engine-template"),
		Prompt:           "Hello, this is also a plain text template",
		Engine:           "",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Kind:             "",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template
	result, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		ID:               created.Template.ID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello, this is also a plain text template", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplate_Mustache_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a mustache template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("mustache-template"),
		Prompt:           "Hello {{name}}! You are {{age}} years old and live in {{city}}.",
		Engine:           "mustache",
		Arguments:        conv.Ptr(`{"type": "object", "properties": {"name": {"type": "string"}, "age": {"type": "number"}, "city": {"type": "string"}}, "required": ["name", "age", "city"]}`),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Kind:             "",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template with data
	result, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		ID: created.Template.ID,
		Arguments: map[string]any{
			"name": "John",
			"age":  30,
			"city": "New York",
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello John! You are 30 years old and live in New York.", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplate_Mustache_PartialData(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a mustache template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("partial-data-template"),
		Prompt:           "Hello {{name}}! {{#hasAge}}You are {{age}} years old.{{/hasAge}}{{^hasAge}}Age unknown.{{/hasAge}}",
		Engine:           "mustache",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Kind:             "",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template with partial data
	result, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		ID: created.Template.ID,
		Arguments: map[string]any{
			"name":   "Jane",
			"hasAge": false,
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello Jane! Age unknown.", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplate_Mustache_EmptyArguments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a mustache template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("empty-args-template"),
		Prompt:           "Hello {{name}}! {{#city}}You live in {{city}}.{{/city}}{{^city}}Location unknown.{{/city}}",
		Engine:           "mustache",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Kind:             "",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template with empty arguments
	result, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		ID:               created.Template.ID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello ! Location unknown.", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplate_InvalidTemplateID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to render with invalid UUID
	_, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		ID:               "invalid-uuid",
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "expected error for invalid UUID")
	require.Contains(t, err.Error(), "invalid template id")
}

func TestTemplatesService_RenderTemplate_NonExistentTemplate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to render non-existent template
	nonExistentID := uuid.New().String()
	_, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		ID:               nonExistentID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "expected error for non-existent template")
	require.Contains(t, err.Error(), "template not found")
}

func TestTemplatesService_RenderTemplate_InvalidMustacheTemplate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a template with invalid mustache syntax
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("invalid-mustache-template"),
		Prompt:           "Hello {{name! This is invalid mustache syntax",
		Engine:           "mustache",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Kind:             "",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Try to render the invalid template
	_, err = ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		ID: created.Template.ID,
		Arguments: map[string]any{
			"name": "John",
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "expected error for invalid mustache template")
	require.Contains(t, err.Error(), "failed to render template")
}

// Note: Unsupported engine test is not needed since the database constraint
// prevents creating templates with unsupported engines in the first place.

func TestTemplatesService_RenderTemplate_Mustache_NestedObjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a mustache template with nested object access
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("nested-object-template"),
		Prompt:           "Hello {{user.name}}! Your email is {{user.email}} and you work at {{user.company.name}}.",
		Engine:           "mustache",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Arguments:        nil,
		Kind:             "",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template with nested data
	result, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		ID: created.Template.ID,
		Arguments: map[string]any{
			"user": map[string]any{
				"name":  "Alice",
				"email": "alice@example.com",
				"company": map[string]any{
					"name": "ACME Corp",
				},
			},
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello Alice! Your email is alice@example.com and you work at ACME Corp.", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplate_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestTemplateService(t)

	// Create context without auth
	ctx := t.Context()
	templateID := uuid.New().String()

	_, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		ID:               templateID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "expected error for unauthorized request")
	require.Contains(t, err.Error(), "unauthorized")
}
