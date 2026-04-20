package telemetry_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/rbactest"
)

func TestTelemetry_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = rbactest.WithExactAccessGrants(t, ctx)

	_, err := ti.service.SearchLogs(ctx, &telem_gen.SearchLogsPayload{
		Limit:            10,
		Sort:             "desc",
		Cursor:           nil,
		From:             nil,
		To:               nil,
		Filters:          nil,
		Filter:           nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestTelemetry_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.Grant{Scope: access.ScopeBuildRead, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.SearchLogs(ctx, &telem_gen.SearchLogsPayload{
		Limit:            10,
		Sort:             "desc",
		Cursor:           nil,
		From:             nil,
		To:               nil,
		Filters:          nil,
		Filter:           nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestTelemetry_RBAC_ReadOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.Grant{Scope: access.ScopeBuildWrite, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.SearchLogs(ctx, &telem_gen.SearchLogsPayload{
		Limit:            10,
		Sort:             "desc",
		Cursor:           nil,
		From:             nil,
		To:               nil,
		Filters:          nil,
		Filter:           nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestTelemetry_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = rbactest.WithExactAccessGrants(t, ctx, access.Grant{Scope: access.ScopeBuildRead, Resource: uuid.NewString()})

	_, err := ti.service.SearchLogs(ctx, &telem_gen.SearchLogsPayload{
		Limit:            10,
		Sort:             "desc",
		Cursor:           nil,
		From:             nil,
		To:               nil,
		Filters:          nil,
		Filter:           nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
