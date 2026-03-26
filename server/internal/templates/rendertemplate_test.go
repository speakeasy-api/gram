package templates_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/templates"
	"github.com/speakeasy-api/gram/server/gen/types"
)

func TestTemplatesService_RenderTemplateByID_NoEngine_Success(t *testing.T) {
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
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template by ID
	result, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID:               created.Template.ID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template by ID")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello, this is a plain text template", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplateByID_EmptyEngine_Success(t *testing.T) {
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
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template by ID
	result, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID:               created.Template.ID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template by ID")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello, this is also a plain text template", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplateByID_Mustache_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a mustache template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("mustache-template"),
		Prompt:           "Hello {{name}}! You are {{age}} years old and live in {{city}}.",
		Engine:           "mustache",
		Arguments:        new(`{"type": "object", "properties": {"name": {"type": "string"}, "age": {"type": "number"}, "city": {"type": "string"}}, "required": ["name", "age", "city"]}`),
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      nil,
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template with data by ID
	result, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
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
	require.NoError(t, err, "render template by ID")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello John! You are 30 years old and live in New York.", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplateByID_Mustache_PartialData(t *testing.T) {
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
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template with partial data by ID
	result, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID: created.Template.ID,
		Arguments: map[string]any{
			"name":   "Jane",
			"hasAge": false,
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template by ID")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello Jane! Age unknown.", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplateByID_Mustache_EmptyArguments(t *testing.T) {
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
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template with empty arguments by ID
	result, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID:               created.Template.ID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template by ID")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello ! Location unknown.", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplateByID_InvalidTemplateID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to render with invalid UUID
	_, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID:               "invalid-uuid",
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "expected error for invalid UUID")
	require.Contains(t, err.Error(), "invalid template id")
}

func TestTemplatesService_RenderTemplateByID_NonExistentTemplate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to render non-existent template
	nonExistentID := uuid.New().String()
	_, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID:               nonExistentID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "expected error for non-existent template")
	require.Contains(t, err.Error(), "template not found")
}

func TestTemplatesService_RenderTemplateByID_InvalidMustacheTemplate(t *testing.T) {
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
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Try to render the invalid template by ID
	_, err = ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
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

func TestTemplatesService_RenderTemplateByID_Mustache_NestedObjects(t *testing.T) {
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
		Kind:             "prompt",
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template with nested data by ID
	result, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
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
	require.NoError(t, err, "render template by ID")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello Alice! Your email is alice@example.com and you work at ACME Corp.", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplateByID_HigherOrderTool_JSON_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a higher order tool template with JSON prompt
	jsonPrompt := `{
		"toolName": "summarize_document",
		"purpose": "Create a summary of the provided document",
		"inputs": [
			{
				"name": "document",
				"description": "The document to summarize"
			}
		],
		"steps": [
			{
				"id": "step1",
				"tool": "read_file",
				"canonicalTool": "read_file",
				"instructions": "Read the document content",
				"inputs": ["document"]
			}
		]
	}`

	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("higher-order-json-template"),
		Prompt:           jsonPrompt,
		Engine:           "mustache",
		Kind:             "higher_order_tool",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      new("A test higher order tool"),
		Arguments:        new(`{"type": "object", "properties": {"document": {"type": "string"}}, "required": ["document"]}`),
		ToolsHint:        []string{"read_file"},
	})
	require.NoError(t, err, "create template")

	// Render the template by ID
	result, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID: created.Template.ID,
		Arguments: map[string]any{
			"document": "{{document}}",
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template by ID")

	require.NotNil(t, result, "result is nil")

	// Check that the JSON was converted to XML format
	require.Contains(t, result.Prompt, "The following is a step-by-step plan to achieve a <Purpose>.", "should contain XML header")
	require.Contains(t, result.Prompt, "Do NOT use this tool (summarize_document) again", "should contain tool name")
	require.Contains(t, result.Prompt, "Create a summary of the provided document", "should contain purpose")
	require.Contains(t, result.Prompt, `<Input name="document" description="The document to summarize" />`, "should contain input definition")
	require.Contains(t, result.Prompt, `<CallTool tool_name="read_file">`, "should contain call tool")
	require.Contains(t, result.Prompt, "Read the document content", "should contain step instructions")
	require.Contains(t, result.Prompt, `<Input name="document" />`, "should contain step input")
}

func TestTemplatesService_RenderTemplateByID_HigherOrderTool_JSON_InvalidJSON(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a higher order tool template with invalid JSON prompt
	invalidJSONPrompt := `{
		"toolName": "broken_tool",
		"purpose": "This JSON is missing closing brace"
		// Missing closing brace and other fields
	`

	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("invalid-json-template"),
		Prompt:           invalidJSONPrompt,
		Engine:           "mustache",
		Kind:             "higher_order_tool",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      new("A test higher order tool with invalid JSON"),
		Arguments:        nil,
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Try to render the template with invalid JSON by ID
	_, err = ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID:               created.Template.ID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "expected error for invalid JSON")
	require.Contains(t, err.Error(), "failed to render template")
}

func TestTemplatesService_RenderTemplateByID_HigherOrderTool_JSON_EmptySteps(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a higher order tool template with empty steps
	jsonPrompt := `{
		"toolName": "empty_tool",
		"purpose": "A tool with no steps",
		"inputs": [],
		"steps": []
	}`

	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("empty-steps-template"),
		Prompt:           jsonPrompt,
		Engine:           "mustache",
		Kind:             "higher_order_tool",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      new("A test higher order tool with no steps"),
		Arguments:        new(`{"type": "object", "properties": {}, "required": []}`),
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template by ID
	result, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID:               created.Template.ID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template by ID")

	require.NotNil(t, result, "result is nil")
	require.Contains(t, result.Prompt, "A tool with no steps", "should contain purpose")
	require.Contains(t, result.Prompt, "No inputs needed", "should handle empty inputs")
}

func TestTemplatesService_RenderTemplateByID_RegularPrompt_NotHigherOrderTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a regular template (not higher_order_tool kind) that should not use JSON rendering
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		Name:             types.Slug("regular-template"),
		Prompt:           "Hello {{name}}, this is a regular prompt template",
		Engine:           "mustache",
		Kind:             "prompt", // Not higher_order_tool
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Description:      new("A regular prompt template"),
		Arguments:        new(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`),
		ToolsHint:        []string{},
	})
	require.NoError(t, err, "create template")

	// Render the template by ID
	result, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID: created.Template.ID,
		Arguments: map[string]any{
			"name": "Alice",
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template by ID")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello Alice, this is a regular prompt template", result.Prompt, "should render as regular mustache template")
}

func TestTemplatesService_RenderTemplateByID_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestTemplateService(t)

	// Create context without auth
	ctx := t.Context()
	templateID := uuid.New().String()

	_, err := ti.service.RenderTemplateByID(ctx, &gen.RenderTemplateByIDPayload{
		ID:               templateID,
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "expected error for unauthorized request")
	require.Contains(t, err.Error(), "unauthorized")
}

func TestTemplatesService_RenderTemplate_DirectPrompt_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Render template directly with prompt parameter (no database lookup)
	result, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		Prompt: "Hello {{name}}! You are {{age}} years old.",
		Engine: "mustache",
		Kind:   "prompt",
		Arguments: map[string]any{
			"name": "Bob",
			"age":  25,
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template with direct prompt")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello Bob! You are 25 years old.", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplate_Mustache_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Render template directly with mustache engine
	result, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		Prompt: "Hello {{name}}! You are {{age}} years old.",
		Engine: "mustache",
		Kind:   "prompt",
		Arguments: map[string]any{
			"name": "Charlie",
			"age":  30,
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template with mustache")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello Charlie! You are 30 years old.", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplate_NoEngine_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Render template with no engine (plain text)
	result, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		Prompt:           "Hello world, this is plain text",
		Engine:           "",
		Kind:             "prompt",
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template with no engine")

	require.NotNil(t, result, "result is nil")
	require.Equal(t, "Hello world, this is plain text", result.Prompt, "rendered prompt mismatch")
}

func TestTemplatesService_RenderTemplate_HigherOrderTool_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// JSON prompt for higher order tool
	jsonPrompt := `{
		"toolName": "test_tool",
		"purpose": "Test purpose",
		"inputs": [
			{
				"name": "input1",
				"description": "Test input"
			}
		],
		"steps": [
			{
				"id": "step1",
				"tool": "test_action",
				"canonicalTool": "test_action",
				"instructions": "Do something",
				"inputs": ["input1"]
			}
		]
	}`

	// Render template directly with higher order tool kind
	result, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		Prompt:           jsonPrompt,
		Engine:           "mustache",
		Kind:             "higher_order_tool",
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "render template with higher order tool")

	require.NotNil(t, result, "result is nil")
	require.Contains(t, result.Prompt, "It relies on executing other tools available in context to achieve the desired purpose", "should contain XML header")
	require.Contains(t, result.Prompt, "Test purpose", "should contain purpose")
	require.Contains(t, result.Prompt, "Do NOT use this tool (test_tool) again", "should contain tool name")
}

func TestTemplatesService_RenderTemplate_UnsupportedEngine_Error(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to render with unsupported engine
	_, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		Prompt:           "Hello {{name}}",
		Engine:           "unsupported_engine",
		Kind:             "prompt",
		Arguments:        map[string]any{"name": "test"},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "should fail with unsupported engine")
	require.Contains(t, err.Error(), "unsupported template engine")
}

func TestTemplatesService_RenderTemplate_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestTemplateService(t)

	// Create context without auth
	ctx := t.Context()

	_, err := ti.service.RenderTemplate(ctx, &gen.RenderTemplatePayload{
		Prompt:           "Hello world",
		Engine:           "mustache",
		Kind:             "prompt",
		Arguments:        map[string]any{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "expected error for unauthorized request")
	require.Contains(t, err.Error(), "unauthorized")
}
