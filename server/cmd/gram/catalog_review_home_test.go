package gram

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

func TestResolveCatalogReviewHome(t *testing.T) {
	t.Parallel()
	const projectID = "00000000-0000-7000-8000-00000000f00d"

	home, enabled, err := resolveCatalogReviewHome("prod", "", "")
	require.NoError(t, err)
	require.True(t, enabled)
	require.Equal(t, constants.SpeakeasyTeamOrganizationID, home.OrganizationID)

	_, enabled, err = resolveCatalogReviewHome("dev", "", "")
	require.NoError(t, err)
	require.False(t, enabled, "non-production never falls back to the production organization")

	home, enabled, err = resolveCatalogReviewHome("dev", "org-synthetic", projectID)
	require.NoError(t, err)
	require.True(t, enabled)
	require.Equal(t, "org-synthetic", home.OrganizationID)

	_, _, err = resolveCatalogReviewHome("dev", "", projectID)
	require.Error(t, err)
	_, _, err = resolveCatalogReviewHome("dev", "org-synthetic", "")
	require.Error(t, err)
	_, _, err = resolveCatalogReviewHome("dev", "org-synthetic", "not-a-uuid")
	require.Error(t, err)
}
