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

	// Second call updates the same row (no duplicate) and a partial update
	// that omits Tagline must preserve the previously-stored tagline.
	disabled := false
	resp2, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		Enabled:          &disabled,
	})
	require.NoError(t, err)
	require.False(t, resp2.Enabled)
	require.NotNil(t, resp2.Tagline)
	require.Equal(t, tagline, *resp2.Tagline)
}

func TestUpdatePortal_EmptyStringClears(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tagline := "Initial tagline."
	enabled := true
	resp1, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		Enabled:          &enabled,
		Tagline:          &tagline,
	})
	require.NoError(t, err)
	require.NotNil(t, resp1.Tagline)
	require.Equal(t, tagline, *resp1.Tagline)

	// Explicit clear: passing &"" should NULL the column.
	empty := ""
	resp2, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		Tagline:          &empty,
	})
	require.NoError(t, err)
	require.Nil(t, resp2.Tagline)
}

func TestUpdatePortal_LogoEmptyStringClearsExistingOverride(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Seed an asset that the caller will set as the portal-level logo override.
	overrideAssetID := seedAsset(t, ctx, ti.conn, *authCtx.ProjectID)

	enabled := true
	overrideStr := overrideAssetID.String()
	resp1, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		Enabled:          &enabled,
		LogoAssetID:      &overrideStr,
	})
	require.NoError(t, err)
	require.NotNil(t, resp1.LogoURL)
	require.Contains(t, *resp1.LogoURL, overrideAssetID.String(),
		"logo_url should reference the portal-level override")

	// Explicit clear via empty string must reach the handler now that the
	// Goa UUID format validator no longer rejects "" upstream.
	cleared := ""
	resp2, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		LogoAssetID:      &cleared,
	})
	require.NoError(t, err)
	// No project logo was set, so after clearing the override the resolved
	// logo_url is nil.
	require.Nil(t, resp2.LogoURL)

	// Read again to confirm persistence.
	resp3, err := ti.service.GetPortal(ctx, &gen.GetPortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
	})
	require.NoError(t, err)
	require.Nil(t, resp3.LogoURL)
}

func TestUpdatePortal_InvalidLogoAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	bad := "not-a-uuid"
	_, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		LogoAssetID:      &bad,
	})
	require.Error(t, err)
	require.True(t, isHTTPStatus(err, http.StatusBadRequest))
}
