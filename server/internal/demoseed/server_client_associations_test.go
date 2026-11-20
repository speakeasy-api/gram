//go:build demoseed_safety

package demoseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
)

func TestDemoServerClientAssociations(t *testing.T) {
	t.Parallel()
	testServerClientAssociations(t, DefaultSpec())
}

func TestLocalServerClientAssociations(t *testing.T) {
	t.Parallel()
	testServerClientAssociations(t, LocalSpec())
}

// Check that seed and reseed expose the personal registrations as typed offers.
func testServerClientAssociations(t *testing.T, spec Spec) {
	t.Helper()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	seed := &seeder{spec: spec}
	queries := demoseedtest.New(db)
	for range 2 {
		seedNativePostgres(ctx, t, db, spec)
		for _, server := range []string{"linear", "linear-app", "notion-app"} {
			present, err := queries.HasSeedPersonalOAuthOffer(ctx, demoseedtest.HasSeedPersonalOAuthOfferParams{
				OrgID:          spec.OrgID,
				ServerID:       seed.det("mcpserver-" + server).String(),
				RegistrationID: seed.det("remote-identity-client-" + server).String(),
			})
			require.NoError(t, err)
			require.True(t, present, "%s must offer its personal OAuth registration", server)
		}
	}
}
