package projects_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// The caller acts in speakeasy-team and changes another organization. The
// permission checked is admin of the caller's own organization, because the
// engine refuses an org check about any organization but the caller's.
func TestSetOrganizationWhitelist_ChecksAdminOfTheCallersOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	target := authCtx.ActiveOrganizationID

	staff := *authCtx
	staff.ActiveOrganizationID = constants.SpeakeasyTeamOrganizationID
	staffCtx := contextvalues.SetAuthContext(ctx, &staff)
	payload := &projects.SetOrganizationWhitelistPayload{OrganizationID: target, Whitelisted: true, ApikeyToken: nil}

	admin := authz.GrantsToContext(staffCtx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, constants.SpeakeasyTeamOrganizationID)})
	require.NoError(t, ti.service.SetOrganizationWhitelist(admin, payload))

	// Admin of the target organization is not the permission asked for.
	other := authz.GrantsToContext(staffCtx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, target)})
	err := ti.service.SetOrganizationWhitelist(other, payload)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
