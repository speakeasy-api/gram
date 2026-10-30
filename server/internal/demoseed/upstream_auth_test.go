//go:build demoseed_safety

package demoseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestSeedUpstreamAuthReadyAfterReseed seeds each tenant twice and checks that
// the second run, whose project delete cascades away every typed auth row the
// first run recorded, leaves nothing for the admin upstream OAuth migration's
// dry runs to do.
func TestSeedUpstreamAuthReadyAfterReseed(t *testing.T) {
	t.Parallel()

	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		t.Run(spec.Marker, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)

			seedLocalPostgres(ctx, t, db, spec)
			seedLocalPostgres(ctx, t, db, spec)

			require.NoError(t, upstreamAuthReadiness(ctx, testenv.NewLogger(t), db, spec.OrgID))

			clients, err := authclientbackfill.Run(ctx, db, authclientbackfill.Options{OrganizationID: spec.OrgID, Apply: false, PageSize: 0})
			require.NoError(t, err)
			require.NotEmpty(t, clients.Results, "the seed records no typed auth clients; readiness is vacuous")
			for _, result := range clients.Results {
				require.Equal(t, authclientbackfill.OutcomeExisting, result.Outcome, "%s auth client for %s", result.Kind, result.SubjectID)
			}
		})
	}
}
