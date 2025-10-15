package variations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/variations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestVariationsService_DeleteGlobal_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// First create a variation to delete
	name := "variation-to-delete"
	created, err := ti.service.UpsertGlobal(ctx, createUpsertPayload("delete-test-tool", &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tool:http:test:delete-test-tool",
		SrcToolName:      "",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             &name,
		Summary:          nil,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
	}))
	require.NoError(t, err, "upsert variation should not error")
	require.NotNil(t, created, "created variation should not be nil")

	// Now delete the variation
	result, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		VariationID:      created.Variation.ID,
	})
	require.NoError(t, err, "delete global variation should not error")
	require.NotNil(t, result, "result should not be nil")
	require.Equal(t, created.Variation.ID, result.VariationID, "returned variation ID should match")

	// Verify the variation is no longer in the list
	listResult, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list variations should not error")

	// The deleted variation should not be in the list
	for _, v := range listResult.Variations {
		require.NotEqual(t, created.Variation.ID, v.ID, "deleted variation should not appear in list")
	}
}

func TestVariationsService_DeleteGlobal_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Try to delete a non-existent variation
	nonExistentID := uuid.New().String()
	_, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		VariationID:      nonExistentID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestVariationsService_DeleteGlobal_InvalidUUID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Try to delete with invalid UUID
	_, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		VariationID:      "invalid-uuid",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid")
}

func TestVariationsService_DeleteGlobal_EmptyUUID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Try to delete with empty UUID
	_, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		VariationID:      "",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid")
}

func TestVariationsService_DeleteGlobal_NilUUID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Try to delete with nil UUID (zero value)
	_, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		VariationID:      uuid.Nil.String(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid")
}

func TestVariationsService_DeleteGlobal_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestVariationsService(t)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		VariationID:      uuid.New().String(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestVariationsService_DeleteGlobal_NoProjectID(t *testing.T) {
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

	_, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		VariationID:      uuid.New().String(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestVariationsService_DeleteGlobal_MultipleDeleteSameID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// First create a variation to delete
	name := "variation-to-delete-twice"
	created, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tool:http:test:delete-twice-tool",
		SrcToolName:      "delete-twice-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             &name,
		Summary:          nil,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
	})
	require.NoError(t, err, "upsert variation should not error")
	require.NotNil(t, created, "created variation should not be nil")

	// First delete should succeed
	result1, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		VariationID:      created.Variation.ID,
	})
	require.NoError(t, err, "first delete should not error")
	require.NotNil(t, result1, "first result should not be nil")
	require.Equal(t, created.Variation.ID, result1.VariationID, "returned variation ID should match")

	// Second delete should fail with not found
	_, err = ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		VariationID:      created.Variation.ID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestVariationsService_DeleteGlobal_DeleteMultipleVariations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Create multiple variations
	variations := []string{"tool1", "tool2", "tool3"}
	createdVars := make([]*gen.UpsertGlobalToolVariationResult, len(variations))

	for i, toolName := range variations {
		name := "variation-to-delete-" + toolName

		created, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			SrcToolUrn:       "tool:http:test:" + toolName,
			SrcToolName:      toolName,
			Confirm:          nil,
			ConfirmPrompt:    nil,
			Name:             &name,
			Summary:          nil,
			Description:      nil,
			Tags:             nil,
			Summarizer:       nil,
		})
		require.NoError(t, err, "upsert variation should not error for %s", toolName)
		createdVars[i] = created
	}

	// Delete each variation
	for i, created := range createdVars {
		result, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			VariationID:      created.Variation.ID,
		})
		require.NoError(t, err, "delete variation %d should not error", i)
		require.NotNil(t, result, "result %d should not be nil", i)
		require.Equal(t, created.Variation.ID, result.VariationID, "returned variation ID should match for variation %d", i)
	}

	// Verify none of the variations are in the list
	listResult, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list variations should not error")

	// None of the deleted variations should be in the list
	for _, v := range listResult.Variations {
		for _, created := range createdVars {
			require.NotEqual(t, created.Variation.ID, v.ID, "deleted variation %s should not appear in list", created.Variation.ID)
		}
	}
}

func TestVariationsService_DeleteGlobal_SoftDelete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Create a variation
	name := "variation-for-soft-delete"
	created, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tool:http:test:soft-delete-tool",
		SrcToolName:      "soft-delete-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             &name,
		Summary:          nil,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
	})
	require.NoError(t, err, "upsert variation should not error")
	require.NotNil(t, created, "created variation should not be nil")

	// Delete the variation
	result, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		VariationID:      created.Variation.ID,
	})
	require.NoError(t, err, "delete variation should not error")
	require.NotNil(t, result, "result should not be nil")

	// Creating a new variation with the same tool name should work
	// (this tests that soft delete allows recreating with same tool name)
	newName := "new-variation-same-tool"
	recreated, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tool:http:test:soft-delete-tool",
		SrcToolName:      "soft-delete-tool", // Same tool name
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             &newName,
		Summary:          nil,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
	})
	require.NoError(t, err, "upsert variation with same tool name should not error after delete")
	require.NotNil(t, recreated, "recreated variation should not be nil")
	require.NotEqual(t, created.Variation.ID, recreated.Variation.ID, "recreated variation should have different ID")
	require.Equal(t, &newName, recreated.Variation.Name, "recreated variation should have new name")
}

func TestVariationsService_DeleteGlobal_ValidUUIDFormat(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Test various valid UUID formats that don't exist
	validUUIDs := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"6ba7b811-9dad-11d1-80b4-00c04fd430c8",
	}

	for _, validUUID := range validUUIDs {
		_, err := ti.service.DeleteGlobal(ctx, &gen.DeleteGlobalPayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			VariationID:      validUUID,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found", "should return not found for valid but non-existent UUID: %s", validUUID)
	}
}

func createUpsertPayload(srcToolName string, overrides *gen.UpsertGlobalPayload) *gen.UpsertGlobalPayload {
	payload := &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tool:http:test:" + srcToolName,
		SrcToolName:      srcToolName,
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             nil,
		Summary:          nil,
		Description:      nil,
		Tags:             nil,
		Summarizer:       nil,
	}

	if overrides != nil {
		if overrides.SrcToolUrn != "" {
			payload.SrcToolUrn = overrides.SrcToolUrn
		}
		if overrides.Confirm != nil {
			payload.Confirm = overrides.Confirm
		}
		if overrides.ConfirmPrompt != nil {
			payload.ConfirmPrompt = overrides.ConfirmPrompt
		}
		if overrides.Name != nil {
			payload.Name = overrides.Name
		}
		if overrides.Summary != nil {
			payload.Summary = overrides.Summary
		}
		if overrides.Description != nil {
			payload.Description = overrides.Description
		}
		if overrides.Tags != nil {
			payload.Tags = overrides.Tags
		}
		if overrides.Summarizer != nil {
			payload.Summarizer = overrides.Summarizer
		}
	}

	return payload
}
