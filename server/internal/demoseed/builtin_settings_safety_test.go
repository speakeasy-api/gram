//go:build demoseed_safety

package demoseed

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
)

func TestBuiltinSlackSettingsReseedAfterProjectDetach(t *testing.T) {
	t.Parallel()
	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		t.Run(spec.OrgID, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			queries := demoseedtest.New(db)
			detach := func(target Spec) {
				t.Helper()
				n, err := queries.DetachBuiltinSlackSettings(ctx, demoseedtest.DetachBuiltinSlackSettingsParams{
					ProjectID: pgtype.UUID{Bytes: uuid.MustParse(target.ProjectID()), Valid: true}, OrgID: target.OrgID,
				})
				require.NoError(t, err)
				require.EqualValues(t, 1, n)
			}

			seedLocalPostgres(ctx, t, db, otherTenantSpec)
			detach(otherTenantSpec)
			outside, err := demoseedtest.SnapshotPostgres(ctx, db)
			require.NoError(t, err)

			seedLocalPostgres(ctx, t, db, spec)
			for range 2 {
				detach(spec)
				// Includes the binding postflight; must recreate the fixed-ID row without
				// a primary-key collision while preserving the other tenant's orphan.
				seedLocalPostgres(ctx, t, db, spec)
				after, err := demoseedtest.SnapshotPostgres(ctx, db)
				require.NoError(t, err)
				requirePostgresRowsPreserved(t, outside, after)
			}
		})
	}
}
