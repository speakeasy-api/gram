package orgprovision_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// TestCreateInWorkOS_AgainstMockWorkOS runs the real WorkOS client against the
// dev identity provider. Every other test in this package and in the admin
// package substitutes the client, so this is the only place that proves the
// sequence works over HTTP: that both routes exist locally, that the SDK's
// request shapes are the ones the mock answers, and that external_id survives a
// round trip. Without it, the local flow can be broken while every test passes.
func TestCreateInWorkOS_AgainstMockWorkOS(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{EnableMockWorkos: true})

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	client := workos.NewClient(guardianPolicy, "dev-idp-mock", workos.ClientOpts{
		Endpoint: idp.MockWorkosURL,
		ClientID: "dev-idp-mock",
	})

	created, err := orgprovision.CreateInWorkOS(t.Context(), client, "Acme Local Co")
	require.NoError(t, err)
	require.NotEmpty(t, created.WorkOSOrganizationID)
	require.Equal(t, orgid.FromWorkOSID(created.WorkOSOrganizationID), created.GramOrganizationID)

	// Reading it back is what proves the back-fill landed rather than being
	// accepted and dropped, which is how a partial mock fails.
	org, err := client.GetOrganization(t.Context(), created.WorkOSOrganizationID)
	require.NoError(t, err)
	require.Equal(t, created.WorkOSOrganizationID, org.ID)
	require.Equal(t, "Acme Local Co", org.Name, "the external_id update must not blank the name")
	require.Equal(t, created.GramOrganizationID, org.ExternalID)

	// Two organizations sharing a name is ordinary, and the mock stores a
	// unique slug, so it must not collapse them onto one row.
	second, err := orgprovision.CreateInWorkOS(t.Context(), client, "Acme Local Co")
	require.NoError(t, err)
	require.NotEqual(t, created.WorkOSOrganizationID, second.WorkOSOrganizationID)
	require.NotEqual(t, created.GramOrganizationID, second.GramOrganizationID)
}
