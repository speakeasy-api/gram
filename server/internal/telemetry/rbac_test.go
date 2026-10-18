package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// logReadEndpoints are the telemetry reads that return individual tool calls or
// raw log records. Each must require log:read on the active project.
var logReadEndpoints = []struct {
	name string
	call func(ctx context.Context, ti *testInstance, check func(error))
}{
	{
		name: "SearchLogs",
		call: func(ctx context.Context, ti *testInstance, check func(error)) {
			_, err := ti.service.SearchLogs(ctx, &telem_gen.SearchLogsPayload{Limit: 10, Sort: "desc"})
			check(err)
		},
	},
	{
		name: "SearchToolCalls",
		call: func(ctx context.Context, ti *testInstance, check func(error)) {
			_, err := ti.service.SearchToolCalls(ctx, &telem_gen.SearchToolCallsPayload{Limit: 10, Sort: "desc"})
			check(err)
		},
	},
	{
		name: "ListToolUsageTraces",
		call: func(ctx context.Context, ti *testInstance, check func(error)) {
			now := time.Now().UTC()
			_, err := ti.service.ListToolUsageTraces(ctx, &telem_gen.ListToolUsageTracesPayload{
				From:  now.Add(-time.Hour).Format(time.RFC3339),
				To:    now.Format(time.RFC3339),
				Limit: 10,
				Sort:  "desc",
			})
			check(err)
		},
	},
	{
		name: "ListHooksTraces",
		call: func(ctx context.Context, ti *testInstance, check func(error)) {
			now := time.Now().UTC()
			_, err := ti.service.ListHooksTraces(ctx, &telem_gen.ListHooksTracesPayload{
				From:  now.Add(-time.Hour).Format(time.RFC3339),
				To:    now.Format(time.RFC3339),
				Limit: 10,
				Sort:  "desc",
			})
			check(err)
		},
	},
}

func requireForbidden(t *testing.T, err error) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestTelemetry_RBAC_LogReads_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	for _, endpoint := range logReadEndpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestLogsService(t)
			ctx = authztest.WithExactGrants(t, ctx)

			endpoint.call(ctx, ti, func(err error) { requireForbidden(t, err) })
		})
	}
}

// project access and org:admin do not imply log:read: raw tool-call logs carry
// every member's tool arguments, results, and prompts.
func TestTelemetry_RBAC_LogReads_DeniedWithProjectAndOrgAdminGrants(t *testing.T) {
	t.Parallel()

	for _, endpoint := range logReadEndpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestLogsService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			ctx = authztest.WithExactGrants(t, ctx,
				authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
				authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()),
				authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
			)

			endpoint.call(ctx, ti, func(err error) { requireForbidden(t, err) })
		})
	}
}

func TestTelemetry_RBAC_LogReads_AllowedWithLogReadGrant(t *testing.T) {
	t.Parallel()

	for _, endpoint := range logReadEndpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestLogsService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeLogRead, authCtx.ProjectID.String()))

			endpoint.call(ctx, ti, func(err error) { require.NoError(t, err) })
		})
	}
}

func TestTelemetry_RBAC_LogReads_DeniedWithOtherProjectLogRead(t *testing.T) {
	t.Parallel()

	for _, endpoint := range logReadEndpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestLogsService(t)
			ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeLogRead, uuid.NewString()))

			endpoint.call(ctx, ti, func(err error) { requireForbidden(t, err) })
		})
	}
}

// Session summaries stay project-scoped aggregates, so project:read alone
// still reaches them.
func TestTelemetry_RBAC_SearchChats_AllowedWithProjectReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

	_, err := ti.service.SearchChats(ctx, &telem_gen.SearchChatsPayload{Limit: 10, Sort: "desc"})
	require.NoError(t, err)
}
