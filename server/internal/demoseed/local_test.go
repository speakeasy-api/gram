package demoseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidentity"
	"github.com/speakeasy-api/gram/server/internal/users"
)

func TestResolveDeveloperUsesAuthIdentity(t *testing.T) {
	t.Parallel()

	const email = "developer@example.test"
	dev, err := resolveDeveloper(t.Context(), email)
	require.NoError(t, err)

	workosID := devidentity.WorkOSUserID(devidentity.DeterministicUserID(email))
	require.Equal(t, workosID, dev.WorkOSID)
	require.Equal(t, users.UserIDFromWorkOSID(workosID), dev.ID)
	require.NotEqual(t, devidentity.DeterministicUserID(email).String(), dev.ID)
}
