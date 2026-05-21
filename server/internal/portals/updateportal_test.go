package portals_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/portals"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestUpdatePortal_RequiresProjectWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Remove all grants — enterprise RBAC with nothing allowed.
	ctx = withNoAuthzGrants(t, ctx)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	enabled := true
	_, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		Enabled:          &enabled,
	})
	require.Error(t, err)
	require.True(t, isHTTPStatus(err, http.StatusForbidden))
}

func TestUpdatePortal_Upserts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tagline := "Welcome to our MCP servers."
	enabled := true
	resp1, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		Enabled:          &enabled,
		Tagline:          &tagline,
	})
	require.NoError(t, err)
	require.True(t, resp1.Enabled)
	require.NotNil(t, resp1.Tagline)
	require.Equal(t, tagline, *resp1.Tagline)

	// Second call updates the same row (no duplicate).
	disabled := false
	resp2, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		Enabled:          &disabled,
	})
	require.NoError(t, err)
	require.False(t, resp2.Enabled)
}
