package templates_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/templates"
	"github.com/speakeasy-api/gram/server/gen/types"
)

func TestTemplatesService_ListTemplates_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create multiple templates
	template1, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("list-template-1"),
		Prompt:           "First template prompt",
		Description:      new("First template description"),
		Arguments:        nil,
		Engine:           "mustache",
		Kind:             "prompt",
		ToolsHint:        []string{"assistant"},
	})
	require.NoError(t, err, "create first template")

	template2, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("list-template-2"),
		Prompt:           "Second template prompt",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "higher_order_tool",
		ToolsHint:        nil,
	})
	require.NoError(t, err, "create second template")

	template3, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("list-template-3"),
		Prompt:           "Third template prompt",
		Description:      nil,
		Engine:           "",
		Kind:             "higher_order_tool",
		ToolsHint:        nil,
		Arguments:        new(`{"type": "object", "properties": {"message": {"type": "string"}}, "required": ["message"]}`),
	})
	require.NoError(t, err, "create third template")

	// List all templates
	result, err := ti.service.ListTemplates(ctx, &gen.ListTemplatesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list templates")

	require.NotNil(t, result, "result is nil")
	require.NotNil(t, result.Templates, "templates list is nil")
	require.GreaterOrEqual(t, len(result.Templates), 3, "expected at least 3 templates")

	// Find our created templates in the list
	templateMap := make(map[string]*types.PromptTemplate)
	for _, template := range result.Templates {
		templateMap[template.Name] = template
	}

	// Verify first template
	t1, found := templateMap["list-template-1"]
	require.True(t, found, "first template not found in list")
	require.Equal(t, template1.Template.ID, t1.ID, "first template ID mismatch")
	require.Equal(t, "First template prompt", t1.Prompt, "first template prompt mismatch")
	require.Equal(t, "First template description", t1.Description, "first template description mismatch")
	require.Equal(t, "mustache", t1.Engine, "first template engine mismatch")
	require.Equal(t, "prompt", t1.Kind, "first template kind mismatch")
	require.ElementsMatch(t, []string{"assistant"}, t1.ToolsHint, "first template tools hint mismatch")

	// Verify second template
	t2, found := templateMap["list-template-2"]
	require.True(t, found, "second template not found in list")
	require.Equal(t, template2.Template.ID, t2.ID, "second template ID mismatch")
	require.Equal(t, "Second template prompt", t2.Prompt, "second template prompt mismatch")
	require.Empty(t, t2.Engine, "second template engine mismatch")
	require.Equal(t, "higher_order_tool", t2.Kind, "second template kind mismatch")

	// Verify third template
	t3, found := templateMap["list-template-3"]
	require.True(t, found, "third template not found in list")
	require.Equal(t, template3.Template.ID, t3.ID, "third template ID mismatch")
	require.Equal(t, "Third template prompt", t3.Prompt, "third template prompt mismatch")
	require.JSONEq(t, `{"type": "object", "properties": {"message": {"type": "string"}}, "required": ["message"]}`, t3.Schema, "third template arguments mismatch")
}

func TestTemplatesService_ListTemplates_EmptyList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// List templates when none exist for this project
	result, err := ti.service.ListTemplates(ctx, &gen.ListTemplatesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list templates")

	require.NotNil(t, result, "result is nil")
	require.NotNil(t, result.Templates, "templates list is nil")
	require.Empty(t, result.Templates, "expected empty templates list")
}

func TestTemplatesService_ListTemplates_SingleTemplate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create a single template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("single-list-template"),
		Prompt:           "Only template in the project",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
	})
	require.NoError(t, err, "create template")

	// List templates
	result, err := ti.service.ListTemplates(ctx, &gen.ListTemplatesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list templates")

	require.NotNil(t, result, "result is nil")
	require.NotNil(t, result.Templates, "templates list is nil")
	require.Len(t, result.Templates, 1, "expected exactly 1 template")

	template := result.Templates[0]
	require.Equal(t, created.Template.ID, template.ID, "template ID mismatch")
	require.Equal(t, "single-list-template", template.Name, "template name mismatch")
	require.Equal(t, "Only template in the project", template.Prompt, "template prompt mismatch")
}

func TestTemplatesService_ListTemplates_DeletedTemplateNotIncluded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Create two templates
	_, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("keep-template"),
		Prompt:           "Template to keep",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
	})
	require.NoError(t, err, "create template to keep")

	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("delete-template"),
		Prompt:           "Template to delete",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
	})
	require.NoError(t, err, "create template to delete")

	// Delete one template
	err = ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new(created.Template.ID),
		Name:             nil,
	})
	require.NoError(t, err, "delete template")

	// List templates
	result, err := ti.service.ListTemplates(ctx, &gen.ListTemplatesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list templates")

	require.NotNil(t, result, "result is nil")
	require.NotNil(t, result.Templates, "templates list is nil")
	require.Len(t, result.Templates, 1, "expected exactly 1 template after deletion")

	template := result.Templates[0]
	require.Equal(t, "keep-template", template.Name, "wrong template remained after deletion")
}

func TestTemplatesService_ListTemplates_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestTemplateService(t)

	// Create context without auth
	ctx := t.Context()

	_, err := ti.service.ListTemplates(ctx, &gen.ListTemplatesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err, "expected error for unauthorized request")
	require.Contains(t, err.Error(), "unauthorized")
}
