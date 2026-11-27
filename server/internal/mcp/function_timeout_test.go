package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/functions"
)

// timeoutRecordingRunner records the invocation limit the gateway hands the
// runner, then answers like a deployment without Functions.
type timeoutRecordingRunner struct {
	toolSeconds     atomic.Int64
	resourceSeconds atomic.Int64
}

func (r *timeoutRecordingRunner) ToolCall(_ context.Context, req functions.RunnerToolCallRequest) (*http.Request, error) {
	r.toolSeconds.Store(int64(req.TimeoutSeconds))
	return nil, functions.ErrFunctionsDisabled
}

func (r *timeoutRecordingRunner) ReadResource(_ context.Context, req functions.RunnerResourceReadRequest) (*http.Request, error) {
	r.resourceSeconds.Store(int64(req.TimeoutSeconds))
	return nil, functions.ErrFunctionsDisabled
}

func callFunctionToolAndResource(t *testing.T, storedSeconds *int) (toolSeconds, resourceSeconds int) {
	t.Helper()

	runner := &timeoutRecordingRunner{}
	ctx, ti, slug, toolName, resourceURI := seedFunctionsServer(t, runner)
	if storedSeconds != nil {
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		_, err := adminrepo.New(ti.conn).AdminSetOrganizationFunctionTimeout(ctx, adminrepo.AdminSetOrganizationFunctionTimeoutParams{
			ID:                     authCtx.ActiveOrganizationID,
			FunctionTimeoutSeconds: pgtype.Int4{Int32: int32(*storedSeconds), Valid: true},
		})
		require.NoError(t, err)
	}

	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": toolName, "arguments": map[string]any{}})
	_, err := servePublicHTTP(t, t.Context(), ti, slug, body, "", nil)
	require.NoError(t, err)

	body = makeMetaRPCBody(t, "resources/read", map[string]any{"uri": resourceURI})
	_, err = servePublicHTTP(t, t.Context(), ti, slug, body, "", nil)
	require.NoError(t, err)

	return int(runner.toolSeconds.Load()), int(runner.resourceSeconds.Load())
}

func TestFunctionCallsUseOrganizationTimeout(t *testing.T) {
	t.Parallel()

	// 600 sits above the runner's old 5-minute cap, so a clamp there fails.
	for _, stored := range []int{30, 240, 600} {
		t.Run(fmt.Sprintf("%d seconds", stored), func(t *testing.T) {
			t.Parallel()
			toolSeconds, resourceSeconds := callFunctionToolAndResource(t, new(stored))
			require.Equal(t, stored, toolSeconds)
			require.Equal(t, stored, resourceSeconds)
		})
	}
}

func TestFunctionCallsDefaultToOneMinute(t *testing.T) {
	t.Parallel()

	toolSeconds, resourceSeconds := callFunctionToolAndResource(t, nil)
	require.Equal(t, 60, toolSeconds)
	require.Equal(t, 60, resourceSeconds)
}
