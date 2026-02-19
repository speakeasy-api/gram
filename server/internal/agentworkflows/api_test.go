package agentworkflows_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agentworkflows"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestAgentsAPIService_CreateResponse(t *testing.T) {
	t.Parallel()

	t.Run("returns error without auth context", func(t *testing.T) {
		t.Parallel()
		_, ti := newTestAgentsAPIService(t)

		// Create a context without auth
		ctxWithoutAuth := t.Context()

		_, err := ti.service.CreateResponse(ctxWithoutAuth, &gen.CreateResponsePayload{
			Model: "openai/gpt-4o",
			Input: "Hello, agent!",
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("starts async workflow and returns response ID", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestAgentsAPIService(t)

		// Verify auth context has project ID set (from Gram-Project header)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		async := true
		resp, err := ti.service.CreateResponse(ctx, &gen.CreateResponsePayload{
			Model: "openai/gpt-4o",
			Input: "Hello, agent!",
			Async: &async,
		})
		require.NoError(t, err)

		// Verify async response fields
		require.NotEmpty(t, resp.ID)
		require.Equal(t, "response", resp.Object)
		require.Equal(t, "in_progress", resp.Status)
		require.NotZero(t, resp.CreatedAt)
		require.Equal(t, "openai/gpt-4o", resp.Model)
		require.Nil(t, resp.Error)
	})

	t.Run("preserves instructions in async response", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestAgentsAPIService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		async := true
		instructions := "You are a helpful assistant."
		resp, err := ti.service.CreateResponse(ctx, &gen.CreateResponsePayload{
			Model:        "openai/gpt-4o",
			Input:        "Hello!",
			Instructions: &instructions,
			Async:        &async,
		})
		require.NoError(t, err)

		require.NotNil(t, resp.Instructions)
		require.Equal(t, instructions, *resp.Instructions)
	})

	t.Run("preserves temperature in async response", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestAgentsAPIService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		async := true
		temperature := 0.7
		resp, err := ti.service.CreateResponse(ctx, &gen.CreateResponsePayload{
			Model:       "openai/gpt-4o",
			Input:       "Hello!",
			Temperature: &temperature,
			Async:       &async,
		})
		require.NoError(t, err)

		require.InDelta(t, temperature, resp.Temperature, 0.001)
	})

	t.Run("uses default temperature when not specified", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestAgentsAPIService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		async := true
		resp, err := ti.service.CreateResponse(ctx, &gen.CreateResponsePayload{
			Model: "openai/gpt-4o",
			Input: "Hello!",
			Async: &async,
		})
		require.NoError(t, err)

		// Default temperature is 0.5 when not specified
		require.InDelta(t, 0.5, resp.Temperature, 0.001)
	})

	t.Run("response has correct text format", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestAgentsAPIService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		async := true
		resp, err := ti.service.CreateResponse(ctx, &gen.CreateResponsePayload{
			Model: "openai/gpt-4o",
			Input: "Hello!",
			Async: &async,
		})
		require.NoError(t, err)

		require.NotNil(t, resp.Text)
		require.NotNil(t, resp.Text.Format)
		require.Equal(t, "text", resp.Text.Format.Type)
	})

	t.Run("initializes empty output array for async response", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestAgentsAPIService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		async := true
		resp, err := ti.service.CreateResponse(ctx, &gen.CreateResponsePayload{
			Model: "openai/gpt-4o",
			Input: "Hello!",
			Async: &async,
		})
		require.NoError(t, err)

		require.NotNil(t, resp.Output)
		require.Empty(t, resp.Output)
	})
}

func TestAgentsAPIService_GetResponse(t *testing.T) {
	t.Parallel()

	t.Run("returns error without auth context", func(t *testing.T) {
		t.Parallel()
		_, ti := newTestAgentsAPIService(t)

		// Create a context without auth
		ctxWithoutAuth := t.Context()

		_, err := ti.service.GetResponse(ctxWithoutAuth, &gen.GetResponsePayload{
			ResponseID: "test-response-id",
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("returns error for non-existent workflow", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestAgentsAPIService(t)

		_, err := ti.service.GetResponse(ctx, &gen.GetResponsePayload{
			ResponseID: "non-existent-workflow-id",
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeNotFound, oopsErr.Code)
	})

	t.Run("returns in-progress status for running workflow", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestAgentsAPIService(t)

		// Verify auth context has project ID set (from Gram-Project header)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		// First create an async response to get a workflow ID
		async := true
		createResp, err := ti.service.CreateResponse(ctx, &gen.CreateResponsePayload{
			Model: "openai/gpt-4o",
			Input: "Hello, agent!",
			Async: &async,
		})
		require.NoError(t, err)
		require.NotEmpty(t, createResp.ID)

		// Now query the response status
		getResp, err := ti.service.GetResponse(ctx, &gen.GetResponsePayload{
			ResponseID: createResp.ID,
		})
		require.NoError(t, err)

		require.Equal(t, createResp.ID, getResp.ID)
		require.Equal(t, "response", getResp.Object)
		// The status should be "in_progress" initially, though it may complete quickly
		require.Contains(t, []string{"in_progress", "completed", "failed"}, getResp.Status)
	})
}
