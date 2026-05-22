package portals_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/portals"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestGetPortal_OtherOrg_Returns404 verifies that a caller authenticated to
// one organization cannot read another organization's portal. The previous
// version of this test had a sibling org querying its own (non-existent)
// portal, which 404'd for the wrong reason. The real cross-org isolation
// happens at the project-slug security scheme: APIKeyAuth invokes
// checkProjectAccess which rejects the request because the slug does not
// belong to the caller's org.
//
// Deviation: the project-slug middleware returns Forbidden (403), not
// NotFound (404), when the caller doesn't own the slug. The test asserts
// against the system's actual behavior.
func TestGetPortal_OtherOrg_Returns404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Enable the original instance's portal so "row exists, enabled=true" —
	// guarantees the would-be 404-from-disabled path is not what protects us.
	originalAuthCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, originalAuthCtx.ProjectSlug)
	originalProjectSlug := *originalAuthCtx.ProjectSlug

	enabled := true
	_, err := ti.service.UpdatePortal(ctx, &gen.UpdatePortalPayload{
		ProjectSlugInput: originalAuthCtx.ProjectSlug,
		Enabled:          &enabled,
	})
	require.NoError(t, err)

	// Authenticate as a sibling org and try to access the original org's slug.
	siblingCtx := newSiblingOrgContext(t, ti.conn, ti.sessionManager)

	// Invoke the project-slug security scheme exactly like Goa's generated
	// endpoint wrapper does. This is the cross-org gate that the unit-level
	// GetPortal/UpdatePortal handlers themselves do not run.
	_, err = ti.service.APIKeyAuth(siblingCtx, originalProjectSlug, &security.APIKeyScheme{
		Name:           constants.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{},
	})
	require.Error(t, err, "sibling org must not be allowed to access another org's project slug")
	requireOopsCode(t, err, oops.CodeForbidden)
}
