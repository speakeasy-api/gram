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
)

// ownScopeEndpoints are the telemetry reads whose rows name a member: raw tool
// calls and log records behind log:read, chat session summaries behind
// chat:read. Each reads the whole project with its wide scope and only the
// caller's own scope without it.
var ownScopeEndpoints = []struct {
	name      string
	wideScope authz.Scope
	call      func(ctx context.Context, ti *testInstance, check func(error))
}{
	{
		name:      "SearchLogs",
		wideScope: authz.ScopeLogRead,
		call: func(ctx context.Context, ti *testInstance, check func(error)) {
			_, err := ti.service.SearchLogs(ctx, &telem_gen.SearchLogsPayload{Limit: 10, Sort: "desc"})
			check(err)
		},
	},
	{
		name:      "SearchToolCalls",
		wideScope: authz.ScopeLogRead,
		call: func(ctx context.Context, ti *testInstance, check func(error)) {
			_, err := ti.service.SearchToolCalls(ctx, &telem_gen.SearchToolCallsPayload{Limit: 10, Sort: "desc"})
			check(err)
		},
	},
	{
		name:      "ListToolUsageTraces",
		wideScope: authz.ScopeLogRead,
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
		name:      "ListHooksTraces",
		wideScope: authz.ScopeLogRead,
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
	{
		name:      "SearchChats",
		wideScope: authz.ScopeChatRead,
		call: func(ctx context.Context, ti *testInstance, check func(error)) {
			_, err := ti.service.SearchChats(ctx, &telem_gen.SearchChatsPayload{Limit: 10, Sort: "desc"})
			check(err)
		},
	},
}

// A member without the wide scope is not refused; the read falls back to their
// own scope (see own_scope_test.go).
func TestTelemetry_RBAC_OwnScopeReads_NoGrantsFallsBackToOwnScope(t *testing.T) {
	t.Parallel()

	for _, endpoint := range ownScopeEndpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestLogsService(t)
			ctx = authztest.WithExactGrants(t, ctx)

			endpoint.call(ctx, ti, func(err error) { require.NoError(t, err) })
		})
	}
}

// Project access and org:admin imply neither log:read nor chat:read; such
// callers read only their own scope.
func TestTelemetry_RBAC_OwnScopeReads_ProjectAndOrgAdminGrantsStayInOwnScope(t *testing.T) {
	t.Parallel()

	for _, endpoint := range ownScopeEndpoints {
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

			endpoint.call(ctx, ti, func(err error) { require.NoError(t, err) })
		})
	}
}

func TestTelemetry_RBAC_OwnScopeReads_AllowedWithWideGrant(t *testing.T) {
	t.Parallel()

	for _, endpoint := range ownScopeEndpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestLogsService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(endpoint.wideScope, authCtx.ProjectID.String()))

			endpoint.call(ctx, ti, func(err error) { require.NoError(t, err) })
		})
	}
}

// The wide scope on another project does not widen this project's reads.
func TestTelemetry_RBAC_OwnScopeReads_OtherProjectWideGrantStaysInOwnScope(t *testing.T) {
	t.Parallel()

	for _, endpoint := range ownScopeEndpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestLogsService(t)
			ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(endpoint.wideScope, uuid.NewString()))

			endpoint.call(ctx, ti, func(err error) { require.NoError(t, err) })
		})
	}
}

// No grant shape short of the wide scope on this project widens these reads
// past the caller's own scope. One fixture covers all five reads.
func TestTelemetry_RBAC_OwnScopeReads_UnrelatedGrantsStayInOwnScope(t *testing.T) {
	t.Parallel()

	grants := map[string]func(authCtx *contextvalues.AuthContext) []authz.Grant{
		"no grants": func(*contextvalues.AuthContext) []authz.Grant { return nil },
		"project and org admin": func(authCtx *contextvalues.AuthContext) []authz.Grant {
			return []authz.Grant{
				authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
				authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()),
				authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
			}
		},
		"wide scopes on another project": func(*contextvalues.AuthContext) []authz.Grant {
			other := uuid.NewString()
			return []authz.Grant{
				authz.NewGrant(authz.ScopeLogRead, other),
				authz.NewGrant(authz.ScopeChatRead, other),
			}
		},
	}
	for name, grantsFor := range grants {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestLogsService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			ctx = authztest.WithExactGrants(t, ctx, grantsFor(authCtx)...)
			requireOwnScopeOnly(t, ctx, ti)
		})
	}
}

// log:read reaches raw logs but not chat summaries, and chat:read the reverse.
func TestTelemetry_RBAC_OwnScopeReads_WideScopesDoNotCrossOver(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := seedOwnScopeFixture(t, ctx, ti, authCtx)

	logReader := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeLogRead, authCtx.ProjectID.String()))
	require.True(t, toolNamesVisible(t, logReader, ti, f)["SearchToolCalls"][f.otherTool])
	require.Nil(t, chatsVisible(t, logReader, ti, f)[f.otherChat], "log:read leaked another member's chat")

	chatReader := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, authCtx.ProjectID.String()))
	require.NotNil(t, chatsVisible(t, chatReader, ti, f)[f.otherChat])
	require.False(t, toolNamesVisible(t, chatReader, ti, f)["SearchToolCalls"][f.otherTool], "chat:read leaked another member's tool call")
}
