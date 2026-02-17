package variations_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/variations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestVariationsService_UpsertGlobal_Create(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	name := "test-variation"
	summary := "test summary"
	description := "test description"
	confirm := "always"
	confirmPrompt := "Are you sure?"
	summarizer := "default"
	tags := []string{"test", "variation"}

	result, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:test-tool",
		SrcToolName:      "test-tool",
		Confirm:          &confirm,
		ConfirmPrompt:    &confirmPrompt,
		Name:             &name,
		Summary:          &summary,
		Description:      &description,
		Tags:             tags,
		Summarizer:       &summarizer,
	})
	require.NoError(t, err, "upsert global variation should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variation, "variation should not be nil")

	// Verify variation fields
	require.NotEmpty(t, result.Variation.ID, "ID should not be empty")
	require.NotEmpty(t, result.Variation.GroupID, "group ID should not be empty")
	require.Equal(t, "test-tool", result.Variation.SrcToolName, "src tool name should match")
	require.Equal(t, "tools:http:test:test-tool", result.Variation.SrcToolUrn, "src tool urn should match")
	require.Equal(t, &confirm, result.Variation.Confirm, "confirm should match")
	require.Equal(t, &confirmPrompt, result.Variation.ConfirmPrompt, "confirm prompt should match")
	require.Equal(t, &name, result.Variation.Name, "name should match")
	require.Equal(t, &description, result.Variation.Description, "description should match")
	require.Equal(t, &summarizer, result.Variation.Summarizer, "summarizer should match")
	require.NotEmpty(t, result.Variation.CreatedAt, "created at should not be empty")
	require.NotEmpty(t, result.Variation.UpdatedAt, "updated at should not be empty")
}

func TestVariationsService_UpsertGlobal_Update(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Create initial variation
	initialName := "initial-variation"
	initialSummary := "initial summary"

	first, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:test-tool",
		SrcToolName:      "test-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             &initialName,
		Summary:          &initialSummary,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
	})
	require.NoError(t, err, "first upsert should not error")
	require.NotNil(t, first, "first result should not be nil")

	// Update the same tool variation
	updatedName := "updated-variation"
	updatedSummary := "updated summary"
	updatedDescription := "updated description"
	updatedConfirm := "never"
	updatedTags := []string{"updated", "tags"}

	second, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:test-tool",
		SrcToolName:      "test-tool", // Same tool name - should update
		Confirm:          &updatedConfirm,
		ConfirmPrompt:    nil,
		Name:             &updatedName,
		Summary:          &updatedSummary,
		Description:      &updatedDescription,
		Tags:             updatedTags,
		Summarizer:       nil,
	})
	require.NoError(t, err, "second upsert should not error")
	require.NotNil(t, second, "second result should not be nil")

	// Should be the same variation (same ID and GroupID)
	require.Equal(t, first.Variation.ID, second.Variation.ID, "variation ID should be the same")
	require.Equal(t, first.Variation.GroupID, second.Variation.GroupID, "group ID should be the same")

	// Values should be updated
	require.Equal(t, "test-tool", second.Variation.SrcToolName, "src tool name should match")
	require.Equal(t, "tools:http:test:test-tool", second.Variation.SrcToolUrn, "src tool urn should match")
	require.Equal(t, &updatedConfirm, second.Variation.Confirm, "confirm should be updated")
	require.Equal(t, &updatedName, second.Variation.Name, "name should be updated")
	require.Equal(t, &updatedDescription, second.Variation.Description, "description should be updated")

	// Updated at should be greater or equal (it could be the same if update happens very quickly)
	require.GreaterOrEqual(t, second.Variation.UpdatedAt, first.Variation.UpdatedAt, "updated at should be greater or equal after update")
}

func TestVariationsService_UpsertGlobal_MinimalPayload(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	result, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:minimal-tool",
		SrcToolName:      "minimal-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             nil,
		Summary:          nil,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
		// Only required field provided, all optional fields nil
	})
	require.NoError(t, err, "upsert with minimal payload should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variation, "variation should not be nil")

	// Verify variation fields
	require.NotEmpty(t, result.Variation.ID, "ID should not be empty")
	require.NotEmpty(t, result.Variation.GroupID, "group ID should not be empty")
	require.Equal(t, "minimal-tool", result.Variation.SrcToolName, "src tool name should match")
	require.Equal(t, "tools:http:test:minimal-tool", result.Variation.SrcToolUrn, "src tool urn should match")
	require.Nil(t, result.Variation.Confirm, "confirm should be nil")
	require.Nil(t, result.Variation.ConfirmPrompt, "confirm prompt should be nil")
	require.Nil(t, result.Variation.Name, "name should be nil")
	require.Nil(t, result.Variation.Description, "description should be nil")
	require.Nil(t, result.Variation.Summarizer, "summarizer should be nil")
	require.NotEmpty(t, result.Variation.CreatedAt, "created at should not be empty")
	require.NotEmpty(t, result.Variation.UpdatedAt, "updated at should not be empty")
}

func TestVariationsService_UpsertGlobal_EmptyTags(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	result, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:empty-tags-tool",
		SrcToolName:      "empty-tags-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             nil,
		Summary:          nil,
		Description:      nil,
		Tags:             []string{}, // Empty tags array
		Summarizer:       nil,
	})
	require.NoError(t, err, "upsert with empty tags should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variation, "variation should not be nil")

	require.Equal(t, "empty-tags-tool", result.Variation.SrcToolName, "src tool name should match")
	require.Equal(t, "tools:http:test:empty-tags-tool", result.Variation.SrcToolUrn, "src tool urn should match")
}

func TestVariationsService_UpsertGlobal_NilTags(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	result, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:nil-tags-tool",
		SrcToolName:      "nil-tags-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             nil,
		Summary:          nil,
		Description:      nil,
		Tags:             nil, // Nil tags
		Summarizer:       nil,
	})
	require.NoError(t, err, "upsert with nil tags should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variation, "variation should not be nil")

	require.Equal(t, "nil-tags-tool", result.Variation.SrcToolName, "src tool name should match")
	require.Equal(t, "tools:http:test:nil-tags-tool", result.Variation.SrcToolUrn, "src tool urn should match")
}

func TestVariationsService_UpsertGlobal_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestVariationsService(t)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:test-tool",
		SrcToolName:      "test-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             nil,
		Summary:          nil,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestVariationsService_UpsertGlobal_NoProjectID(t *testing.T) {
	t.Parallel()

	_, ti := newTestVariationsService(t)

	// Create context with auth but no project ID
	ctx := t.Context()
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: "test-org",
		UserID:               "test-user",
		SessionID:            nil,
		ProjectID:            nil, // No project ID
		OrganizationSlug:     "test-org",
		Email:                nil,
		AccountType:          "free",
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	}
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:test-tool",
		SrcToolName:      "test-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             nil,
		Summary:          nil,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestVariationsService_UpsertGlobal_CreatesGroup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Create first variation - should create the group
	first, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:first-tool",
		SrcToolName:      "first-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             new("first variation"),
		Summary:          nil,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
	})
	require.NoError(t, err, "first upsert should not error")
	require.NotNil(t, first, "first result should not be nil")

	// Create second variation - should use the same group
	second, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:second-tool",
		SrcToolName:      "second-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             new("second variation"),
		Summary:          nil,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
	})
	require.NoError(t, err, "second upsert should not error")
	require.NotNil(t, second, "second result should not be nil")

	// Both variations should belong to the same group
	require.Equal(t, first.Variation.GroupID, second.Variation.GroupID, "both variations should belong to the same group")
}

func TestVariationsService_UpsertGlobal_MultipleToolsSameGroup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Create variations for different tools
	tools := []string{"tool-a", "tool-b", "tool-c"}
	variations := make([]*gen.UpsertGlobalToolVariationResult, len(tools))

	for i, toolName := range tools {
		name := "variation-for-" + toolName

		result, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			SrcToolUrn:       "tools:http:test:" + toolName,
			SrcToolName:      toolName,
			Confirm:          nil,
			ConfirmPrompt:    nil,
			Name:             &name,
			Summary:          nil,
			Description:      nil,
			Tags:             nil,
			Summarizer:       nil,
		})
		require.NoError(t, err, "upsert should not error for %s", toolName)
		require.NotNil(t, result, "result should not be nil for %s", toolName)

		variations[i] = result
	}

	// All variations should belong to the same group
	groupID := variations[0].Variation.GroupID
	for i, variation := range variations {
		require.Equal(t, groupID, variation.Variation.GroupID, "variation %d should belong to the same group", i)
		require.Equal(t, tools[i], variation.Variation.SrcToolName, "variation %d should have correct tool name", i)
	}
}

func TestVariationsService_UpsertGlobal_LongValues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Test with longer values to ensure database can handle them
	longName := "This is a very long variation name that tests the database field limits and ensures we can handle reasonable sized names"
	longSummary := "This is a very long summary that describes the tool variation in great detail and tests that the database can handle longer text fields without truncation or errors occurring during the insert or update operations"
	longDescription := "This is an extremely long description that provides comprehensive documentation about what this tool variation does, how it works, when to use it, and all the various configuration options and parameters that are available. This field should be able to handle much longer text since it's meant to contain detailed documentation and usage instructions for the tool variation."

	result, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:long-values-tool",
		SrcToolName:      "long-values-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             &longName,
		Summary:          &longSummary,
		Description:      &longDescription,
		Tags:             []string{"long", "values", "test", "comprehensive", "detailed"},
		Summarizer:       nil,
	})
	require.NoError(t, err, "upsert with long values should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variation, "variation should not be nil")

	// Verify all long values are preserved
	require.Equal(t, &longName, result.Variation.Name, "long name should be preserved")
	require.Equal(t, &longDescription, result.Variation.Description, "long description should be preserved")
}

func TestVariationsService_UpsertGlobal_EmptyStrings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Test with empty string values
	emptyString := ""

	result, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:empty-strings-tool",
		SrcToolName:      "empty-strings-tool",
		Confirm:          &emptyString,
		ConfirmPrompt:    &emptyString,
		Name:             &emptyString,
		Summary:          &emptyString,
		Description:      &emptyString,
		Tags:             nil,
		Summarizer:       &emptyString,
	})
	require.NoError(t, err, "upsert with empty strings should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variation, "variation should not be nil")

	// Empty strings should be preserved (not converted to nil)
	require.Equal(t, &emptyString, result.Variation.Confirm, "empty confirm should be preserved")
	require.Equal(t, &emptyString, result.Variation.ConfirmPrompt, "empty confirm prompt should be preserved")
	require.Equal(t, &emptyString, result.Variation.Name, "empty name should be preserved")
	require.Equal(t, &emptyString, result.Variation.Description, "empty description should be preserved")
	require.Equal(t, &emptyString, result.Variation.Summarizer, "empty summarizer should be preserved")
}
