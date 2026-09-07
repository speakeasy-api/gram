package orgprovision_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
)

// TestUnavailable_BothMethodsRefuse pins each method separately. Going through
// CreateInWorkOS instead would prove only that something in the sequence
// refused: the second call refusing is enough to make the whole sequence fail,
// so a CreateOrganization that quietly returned a made-up ID and no error would
// still look correct from the outside.
func TestUnavailable_BothMethodsRefuse(t *testing.T) {
	t.Parallel()

	var client orgprovision.WorkOSOrganizationCreator = orgprovision.Unavailable{}

	workosOrgID, err := client.CreateOrganization(t.Context(), "Acme Unavailable Co", "")
	require.ErrorIs(t, err, orgprovision.ErrUnavailable)
	require.Empty(t, workosOrgID, "a refused create must not hand back an organization ID")

	require.ErrorIs(t,
		client.UpdateOrganizationExternalID(t.Context(), "org_01HZUNAVAILABLE", "irrelevant"),
		orgprovision.ErrUnavailable)
}

// TestCreateInWorkOS_UnavailableIsRecognisable pins the sequence's error as one
// callers can still match on, because the admin handler branches on it to
// choose a status code rather than reporting a gateway failure.
func TestCreateInWorkOS_UnavailableIsRecognisable(t *testing.T) {
	t.Parallel()

	_, err := orgprovision.CreateInWorkOS(t.Context(), orgprovision.Unavailable{}, "Acme Unavailable Co")
	require.ErrorIs(t, err, orgprovision.ErrUnavailable)
}
