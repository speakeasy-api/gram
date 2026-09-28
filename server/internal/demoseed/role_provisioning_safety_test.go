//go:build demoseed_safety

package demoseed

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
)

func TestRoleProvisioningReseed(t *testing.T) {
	t.Parallel()
	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		t.Run(spec.OrgID, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)

			seedLocalPostgres(ctx, t, db, otherTenantSpec)
			plantRoleProvisioningHistory(t, db, otherTenantSpec)
			outside, err := demoseedtest.SnapshotPostgres(ctx, db)
			require.NoError(t, err)

			seedLocalPostgres(ctx, t, db, spec)
			associationIDs := plantRoleProvisioningHistory(t, db, spec)
			for range 2 {
				seedLocalPostgres(ctx, t, db, spec)
				var count int
				err = db.QueryRow(ctx, `SELECT count(*) FROM organization_role_provisioning_settings
     WHERE organization_id = $1 AND enabled IS FALSE AND project_id IS NULL AND version = 0`, spec.OrgID).Scan(&count)
				require.NoError(t, err)
				require.Equal(t, 1, count, "reseed must restore default-off organization settings")
				err = db.QueryRow(ctx, `SELECT count(*) FROM role_provisioning_settings WHERE organization_id = $1`, spec.OrgID).Scan(&count)
				require.NoError(t, err)
				require.Zero(t, count, "reseed must remove sticky per-role intent")
				// Check IDs directly: deleting settings first would orphan these
				// rows and make an ownership JOIN incorrectly report no leftovers.
				err = db.QueryRow(ctx, `SELECT count(*) FROM role_plugin_associations WHERE id = ANY($1::uuid[])`, associationIDs).Scan(&count)
				require.NoError(t, err)
				require.Zero(t, count, "reseed must remove all association history")
				after, err := demoseedtest.SnapshotPostgres(ctx, db)
				require.NoError(t, err)
				requirePostgresRowsPreserved(t, outside, after)
			}
		})
	}
}

func plantRoleProvisioningHistory(t *testing.T, db *pgxpool.Pool, spec Spec) []uuid.UUID {
	t.Helper()
	ctx := t.Context()
	projectID := spec.UUIDPrefix + "-0000-4000-a000-000000000001"
	_, err := db.Exec(ctx, `UPDATE organization_role_provisioning_settings
  SET enabled = true, project_id = $2, version = 7 WHERE organization_id = $1`, spec.OrgID, projectID)
	require.NoError(t, err)
	var pluginID uuid.UUID
	err = db.QueryRow(ctx, `INSERT INTO plugins (organization_id, project_id, name, slug)
  VALUES ($1, $2, 'Provisioning safety fixture', 'provisioning-safety-fixture') RETURNING id`, spec.OrgID, projectID).Scan(&pluginID)
	require.NoError(t, err)
	var settingID, pendingID uuid.UUID
	err = db.QueryRow(ctx, `INSERT INTO role_provisioning_settings
  (organization_id, role_urn, project_id, last_attempt_at, last_error_code)
  VALUES ($1, 'role:global:00000000-0000-4000-a000-000000000001', $2, now(), 'fixture_error') RETURNING id`, spec.OrgID, projectID).Scan(&settingID)
	require.NoError(t, err)
	err = db.QueryRow(ctx, `INSERT INTO role_provisioning_settings
  (organization_id, role_urn, enabled, project_id)
  VALUES ($1, 'role:global:00000000-0000-4000-a000-000000000002', false, NULL) RETURNING id`, spec.OrgID).Scan(&pendingID)
	require.NoError(t, err)
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	_, err = db.Exec(ctx, `INSERT INTO role_plugin_associations
  (id, role_provisioning_setting_id, project_id, plugin_id, is_current, retired_at, last_automatic_name)
  VALUES
  ($1, $5, $7, $8, true, NULL, 'Automatic fixture'),
  ($2, $5, $7, NULL, false, now(), NULL),
  ($3, $5, NULL, NULL, false, now(), 'Retired fixture'),
  ($4, $6, NULL, NULL, false, NULL, NULL)`,
		ids[0], ids[1], ids[2], ids[3], settingID, pendingID, projectID, pluginID)
	require.NoError(t, err)
	return ids
}
