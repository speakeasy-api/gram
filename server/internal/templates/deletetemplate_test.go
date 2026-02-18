package templates_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/templates"
	"github.com/speakeasy-api/gram/server/gen/types"
)

func TestTemplatesService_DeleteTemplate_ByID_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template
	created, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("delete-by-id-template"),
		Prompt:           "Template to be deleted by ID",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
	})
	require.NoError(t, err, "create template")

	// Delete the template by ID
	err = ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new(created.Template.ID),
		Name:             nil,
	})
	require.NoError(t, err, "delete template by ID")

	// Verify template is deleted by trying to get it
	_, err = ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new(created.Template.ID),
		Name:             nil,
	})
	require.Error(t, err, "template should not be found after deletion")
	require.Contains(t, err.Error(), "not found")
}

func TestTemplatesService_DeleteTemplate_ByName_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// First create a template
	_, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("delete-by-name-template"),
		Prompt:           "Template to be deleted by name",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "prompt",
		ToolsHint:        nil,
	})
	require.NoError(t, err, "create template")

	// Delete the template by name
	err = ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               nil,
		Name:             new("delete-by-name-template"),
	})
	require.NoError(t, err, "delete template by name")

	// Verify template is deleted by trying to get it
	_, err = ti.service.GetTemplate(ctx, &gen.GetTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               nil,
		Name:             new("delete-by-name-template"),
	})
	require.Error(t, err, "template should not be found after deletion")
	require.Contains(t, err.Error(), "not found")
}

func TestTemplatesService_DeleteTemplate_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to delete with invalid UUID
	err := ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new("invalid-uuid"),
		Name:             nil,
	})
	require.Error(t, err, "expected error for invalid UUID")
	require.Contains(t, err.Error(), "invalid template id")
}

func TestTemplatesService_DeleteTemplate_NilID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to delete with nil UUID (empty string)
	err := ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new(""),
		Name:             nil,
	})
	require.Error(t, err, "expected error for empty UUID")
	require.Contains(t, err.Error(), "either id or name must be provided")
}

func TestTemplatesService_DeleteTemplate_UUIDNil(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to delete with nil UUID value
	err := ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               new(uuid.Nil.String()),
		Name:             nil,
	})
	require.Error(t, err, "expected error for nil UUID")
	require.Contains(t, err.Error(), "invalid template id")
}

func TestTemplatesService_DeleteTemplate_NonExistentID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to delete non-existent template
	nonExistentID := uuid.New().String()
	err := ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               &nonExistentID,
		Name:             nil,
	})
	// Note: The implementation doesn't return an error for non-existent templates
	// This is often a design choice - delete operations can be idempotent
	require.NoError(t, err, "delete of non-existent template should not error")
}

func TestTemplatesService_DeleteTemplate_NonExistentName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to delete non-existent template by name
	err := ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               nil,
		Name:             new("non-existent-template"),
	})
	// Note: The implementation doesn't return an error for non-existent templates
	// This is often a design choice - delete operations can be idempotent
	require.NoError(t, err, "delete of non-existent template should not error")
}

func TestTemplatesService_DeleteTemplate_EmptyName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to delete with empty name
	err := ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               nil,
		Name:             new(""),
	})
	require.Error(t, err, "expected error for empty name")
	require.Contains(t, err.Error(), "either id or name must be provided")
}

func TestTemplatesService_DeleteTemplate_NoIDOrName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	// Try to delete without providing ID or name
	err := ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               nil,
		Name:             nil,
	})
	require.Error(t, err, "expected error when neither ID nor name provided")
	require.Contains(t, err.Error(), "either id or name must be provided")
}

func TestTemplatesService_DeleteTemplate_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestTemplateService(t)

	// Create context without auth
	ctx := t.Context()
	templateID := uuid.New().String()

	err := ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               &templateID,
		Name:             nil,
	})
	require.Error(t, err, "expected error for unauthorized request")
	require.Contains(t, err.Error(), "unauthorized")
}
