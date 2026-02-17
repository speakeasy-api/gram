package variations_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/variations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestVariationsService_ListGlobal_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	result, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list global variations should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variations, "variations should not be nil")
	require.IsType(t, []*types.ToolVariation{}, result.Variations, "variations should be of correct type")
}

func TestVariationsService_ListGlobal_EmptyList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	result, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "should not error when no variations exist")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variations, "variations should not be nil")
	require.Empty(t, result.Variations, "variations should be empty when no variations exist")
}

func TestVariationsService_ListGlobal_WithVariations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// First create a variation to ensure we have something to list
	name := "test-variation"
	summary := "test summary"
	description := "test description"
	confirm := "always"
	confirmPrompt := "Are you sure?"
	summarizer := "test summarizer"
	tags := []string{"test", "variation"}

	created, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
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
	require.NoError(t, err, "upsert variation should not error")
	require.NotNil(t, created, "created variation should not be nil")

	// Now list variations
	result, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list global variations should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variations, "variations should not be nil")
	require.GreaterOrEqual(t, len(result.Variations), 1, "should have at least one variation")

	// Find our created variation
	var foundVar *types.ToolVariation
	for _, v := range result.Variations {
		if v.ID == created.Variation.ID {
			foundVar = v
			break
		}
	}
	require.NotNil(t, foundVar, "should find created variation in list")

	// Verify variation fields
	require.Equal(t, created.Variation.ID, foundVar.ID, "variation ID should match")
	require.Equal(t, created.Variation.GroupID, foundVar.GroupID, "group ID should match")
	require.Equal(t, created.Variation.SrcToolUrn, foundVar.SrcToolUrn, "src tool urn should match")
	require.Equal(t, created.Variation.SrcToolName, foundVar.SrcToolName, "src tool name should match")
	require.Equal(t, created.Variation.Confirm, foundVar.Confirm, "confirm should match")
	require.Equal(t, created.Variation.ConfirmPrompt, foundVar.ConfirmPrompt, "confirm prompt should match")
	require.Equal(t, created.Variation.Name, foundVar.Name, "name should match")
	require.Equal(t, created.Variation.Description, foundVar.Description, "description should match")
	require.Equal(t, created.Variation.Summarizer, foundVar.Summarizer, "summarizer should match")
	require.NotEmpty(t, foundVar.CreatedAt, "created at should not be empty")
	require.NotEmpty(t, foundVar.UpdatedAt, "updated at should not be empty")
}

func TestVariationsService_ListGlobal_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestVariationsService(t)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestVariationsService_ListGlobal_NoProjectID(t *testing.T) {
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

	_, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestVariationsService_ListGlobal_MultipleVariations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Create 50 variations
	numVariations := 50
	createdVars := make([]*gen.UpsertGlobalToolVariationResult, numVariations)

	for i := range numVariations {
		toolName := fmt.Sprintf("tool%d", i+1)
		name := "test-variation-" + toolName
		summary := "test summary for " + toolName
		description := "test description for " + toolName

		created, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			SrcToolUrn:       "tools:http:test:" + toolName,
			SrcToolName:      toolName,
			Confirm:          nil,
			ConfirmPrompt:    nil,
			Name:             &name,
			Summary:          &summary,
			Description:      &description,
			Tags:             []string{toolName},
			Summarizer:       nil,
		})
		require.NoError(t, err, "upsert variation should not error for %s", toolName)
		createdVars[i] = created
	}

	// List variations
	result, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list global variations should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variations, "variations should not be nil")
	require.Len(t, result.Variations, numVariations, "should have at least %d variations", numVariations)

	// Verify all created variations are in the list
	foundVars := make(map[string]bool)
	for _, v := range result.Variations {
		for _, created := range createdVars {
			if v.ID == created.Variation.ID {
				foundVars[created.Variation.ID] = true
				// Verify the variation fields
				require.Equal(t, created.Variation.SrcToolUrn, v.SrcToolUrn, "src tool urn should match for variation %s", created.Variation.ID)
				require.Equal(t, created.Variation.SrcToolName, v.SrcToolName, "src tool name should match for variation %s", created.Variation.ID)
				require.Equal(t, created.Variation.Name, v.Name, "name should match for variation %s", created.Variation.ID)
				require.Equal(t, created.Variation.Description, v.Description, "description should match for variation %s", created.Variation.ID)
			}
		}
	}

	require.Len(t, foundVars, numVariations, "should find all %d created variations in the list", numVariations)
	for _, created := range createdVars {
		require.True(t, foundVars[created.Variation.ID], "variation %s should be found in list", created.Variation.ID)
	}
}

func TestVariationsService_ListGlobal_OrderedByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	// Create variations in sequence
	first, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:first-tool",
		SrcToolName:      "first-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             new("first-variation"),
		Summary:          new("first summary"),
		Description:      new("first description"),
		Tags:             nil,
		Summarizer:       nil,
	})
	require.NoError(t, err, "create first variation")

	time.Sleep(2 * time.Millisecond)

	second, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:test:second-tool",
		SrcToolName:      "second-tool",
		Confirm:          nil,
		ConfirmPrompt:    nil,
		Name:             new("second-variation"),
		Summary:          new("second summary"),
		Description:      new("second description"),
		Tags:             nil,
		Summarizer:       nil,
	})
	require.NoError(t, err, "create second variation")

	// List variations
	result, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list global variations should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Variations, "variations should not be nil")
	require.GreaterOrEqual(t, len(result.Variations), 2, "should have at least 2 variations")

	// Find the positions of our variations in the list
	var firstPos, secondPos = -1, -1
	for i, v := range result.Variations {
		if v.ID == first.Variation.ID {
			firstPos = i
		}
		if v.ID == second.Variation.ID {
			secondPos = i
		}
	}

	require.NotEqual(t, -1, firstPos, "first variation should be found in list")
	require.NotEqual(t, -1, secondPos, "second variation should be found in list")

	// The second variation should appear before the first (DESC order by ID)
	require.Less(t, secondPos, firstPos, "second variation should appear before first variation (DESC order by ID)")
}
