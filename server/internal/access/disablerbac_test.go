package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_DisableRBAC_RejectsPlatformAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	email := "admin@speakeasy.com"
	authCtx.Email = &email
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	err := ti.service.DisableRBAC(ctx, &gen.DisableRBACPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.ErrorContains(t, err, "RBAC cannot be disabled")
}
