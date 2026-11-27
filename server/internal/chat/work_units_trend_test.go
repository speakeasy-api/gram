package chat_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetWorkUnitsTrend_RefusesWithoutChatRead(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithExactGrants(t, initSessionCtx(t, ti))

	_, err := ti.service.GetWorkUnitsTrend(ctx, &gen.GetWorkUnitsTrendPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetWorkUnitsTrend_AllowsProjectChatRead(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, authCtx.ProjectID.String()))

	result, err := ti.service.GetWorkUnitsTrend(ctx, &gen.GetWorkUnitsTrendPayload{})
	require.NoError(t, err)
	require.NotEmpty(t, result.Buckets)
}
