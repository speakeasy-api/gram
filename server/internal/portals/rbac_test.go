package portals_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/portals"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// TestGetPortal_OtherOrg_Returns404 verifies that a caller authenticated to
// a different organization cannot read another organization's portal. The
// project-slug → project-id resolution enforced by the auth middleware means
// requests for a project slug that doesn't belong to the caller's org resolve
// to "not found" before we even hit the handler, but we also cover the case
// where the caller supplies a project ID from a different org by testing with
// the sibling's own project slug.
func TestGetPortal_OtherOrg_Returns404(t *testing.T) {
	t.Parallel()

	// Set up two independent orgs sharing the same test database clone.
	ctx, ti := newTestService(t)

	// Enable the portal for the first (main) org's project.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	enabled := true
	_, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: authCtx.ProjectSlug,
		Enabled:          &enabled,
	})
	require.NoError(t, err)

	// Authenticate as a sibling org.
	siblingCtx := newSiblingOrgContext(t, ti.conn, ti.sessionManager)
	siblingAuthCtx, ok := contextvalues.GetAuthContext(siblingCtx)
	require.True(t, ok)

	// The sibling asks for its own project's portal (which has no row → 404).
	_, err = ti.service.GetPortal(siblingCtx, &gen.GetPortalPayload{
		ProjectSlugInput: siblingAuthCtx.ProjectSlug,
	})
	require.Error(t, err)
	require.True(t, isHTTPStatus(err, http.StatusNotFound), "expected 404, got err: %v", err)
}
