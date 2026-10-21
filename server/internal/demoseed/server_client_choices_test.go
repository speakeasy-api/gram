//go:build demoseed_safety

package demoseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
)

func TestDemoServerClientChoices(t *testing.T) {
	t.Parallel()
	testServerClientChoices(t, DefaultSpec())
}

func TestLocalServerClientChoices(t *testing.T) {
	t.Parallel()
	testServerClientChoices(t, LocalSpec())
}

// Every seeded credential carries the choice issuance would have recorded for
// each server it reaches, so a reseeded organization is already migrated and
// no seeded credential is asked to reconnect.
func testServerClientChoices(t *testing.T, spec Spec) {
	t.Helper()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	for range 2 {
		seedLocalPostgres(ctx, t, db, spec)

		report, err := selectionbackfill.Run(ctx, db, selectionbackfill.Options{OrganizationID: spec.OrgID, Apply: false, PageSize: 0})
		require.NoError(t, err)
		require.Empty(t, report.Gaps, "no seeded credential may be asked to reconnect")
		for outcome, n := range report.Pairs {
			require.Equal(t, selectionbackfill.OutcomeSelected, outcome, "%d pairs are %s: the seed must record every choice", n, outcome)
		}
		require.Equal(t, report.Credentials, report.FullyCovered)
	}
}
