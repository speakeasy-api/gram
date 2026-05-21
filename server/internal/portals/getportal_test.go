package portals_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/portals"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestGetPortal_Disabled_Returns404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// No portal row exists yet (enabled=false by default when no row exists).
	_, err := ti.service.GetPortal(ctx, &gen.GetPortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
	})
	require.Error(t, err)
	require.True(t, isHTTPStatus(err, http.StatusNotFound))
}

func TestGetPortal_Enabled_ReturnsServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	endpointSlug := authCtx.OrganizationSlug + "-weather-mcp"
	seedMcpServerAndEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, endpointSlug)

	// Enable the portal.
	enabled := true
	_, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		Enabled:          &enabled,
	})
	require.NoError(t, err)

	resp, err := ti.service.GetPortal(ctx, &gen.GetPortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
	})
	require.NoError(t, err)
	require.True(t, resp.Enabled)
	require.NotEmpty(t, resp.ProjectSlug)
	require.NotEmpty(t, resp.DisplayName)
	require.Len(t, resp.Servers, 1)
	require.Equal(t, endpointSlug, resp.Servers[0].Slug)
}

func TestGetPortal_PreviewBypassesDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// No portal row; portal is disabled.
	preview := true
	resp, err := ti.service.GetPortal(ctx, &gen.GetPortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		Preview:          &preview,
	})
	// With preview=true and a session that has implicit project:write (free account),
	// the call should succeed and return a disabled portal.
	require.NoError(t, err)
	require.False(t, resp.Enabled)
}
