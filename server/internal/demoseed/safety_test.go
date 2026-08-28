//go:build demoseed_safety

package demoseed

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// otherTenantSpec retargets the embedded seed scripts at a second, unrelated
// tenant. Running the seed "against itself" plants adversarial fixture data in
// exactly the tables the seed touches — including tables added in the future —
// so any unscoped DELETE/UPDATE someone introduces will, by construction, hit
// fixture rows.
//
// It is an ordinary Spec, the same mechanism production uses to seed the local
// development org, so the two cannot drift: a new identifier family added to
// Spec is automatically rewritten here too.
var otherTenantSpec = Spec{
	OrgID:       "org_gram_othr_workspace",
	OrgSlug:     "acme-othr",
	OrgName:     "Acme Othr Org",
	UUIDPrefix:  "0ddba110",
	UserPrefix:  "user_othr_",
	EmailDomain: "@othr.getgram.ai",
	NameSeed:    "gram-othr-",
	GroupPrefix: "othr_grp_",
	WorkOSOrgID: "workos_gram_othr_unlinked",
	Marker:      "othr-seed",
}

func asOtherTenant(t *testing.T, script string) string {
	t.Helper()

	require.NoError(t, otherTenantSpec.Validate())
	return otherTenantSpec.Rewrite(script)
}

// TestDemoSeedSafety is the generic cross-table guard for the demo seed. With
// a second tenant provisioned from a transformed copy of the seed itself, it
// verifies that running the real seed:
//
//  1. never modifies or deletes a single pre-existing row outside the demo
//     scope (full-database row fingerprints in Postgres, per-table
//     count+hash of non-demo rows in ClickHouse);
//  2. cleans up stray demo-scope data left behind between runs (tampered
//     rows are planted after the first run);
//  3. is idempotent — per-table row counts are identical after every run.
func TestDemoSeedSafety(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	blob := assetstest.NewTestBlobStore(t)

	// The critical identifiers must appear in the embedded scripts; if a
	// rename breaks a replacement the fixture silently stops being
	// adversarial, so fail loudly instead.
	for _, needle := range []string{"org_gram_demo_workspace", "dec0de00", "user_demo_"} {
		require.Contains(t, postgresSQL, needle, "postgres.sql lost literal %q; update DefaultSpec", needle)
	}
	for _, needle := range []string{"org_gram_demo_workspace", "dec0de00", "demo-seed"} {
		require.Contains(t, clickhouseSQL, needle, "clickhouse.sql lost literal %q; update DefaultSpec", needle)
	}

	// Rewriting with the default spec must be a no-op, or the production run
	// would not be executing the script as written.
	require.Equal(t, postgresSQL, DefaultSpec().Rewrite(postgresSQL))
	require.Equal(t, clickhouseSQL, DefaultSpec().Rewrite(clickhouseSQL))

	// Baseline BEFORE the fixture: everything the fixture adds on top of it is
	// "another tenant's data", and its ids feed the leak check below.
	pgBaseline, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)

	// Provision the other tenant from the transformed seed scripts, including
	// realistic MCP server dependents that the demo cleanup must not touch.
	require.NoError(t, demoseedtest.ExecPostgresScript(ctx, db, asOtherTenant(t, postgresSQL)))
	require.NoError(t, demoseedtest.PlantMCPServerDependents(
		ctx, db, otherTenantSpec.OrgID, otherTenantSpec.ProjectID(),
	))
	require.NoError(t, demoseedtest.ExecClickHouseStatements(ctx, ch, splitStatements(asOtherTenant(t, clickhouseSQL))))

	demoProjects := []string{DefaultSpec().ProjectID()}

	pgBefore, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)
	fixtureUUIDs := collectUUIDs(addedRows(pgBaseline, pgBefore))
	chBefore, err := demoseedtest.SnapshotClickHouse(ctx, ch, constants.DemoOrganizationID, demoProjects)
	require.NoError(t, err)

	// Nothing is in the demo scope yet: the fixture must be fully outside it,
	// otherwise the isolation assertions below would be vacuous.
	fixtureOutside := false
	for table, state := range chBefore {
		require.Zero(t, state.DemoCount, "clickhouse table %s: fixture rows landed inside the demo scope", table)
		fixtureOutside = fixtureOutside || state.OutsideCount > 0
	}
	require.True(t, fixtureOutside, "fixture produced no clickhouse rows; the isolation check would be vacuous")

	// First real seed run.
	require.NoError(t, Run(ctx, logger, db, ch, blob, DefaultSpec()))

	pgAfter1, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)
	chAfter1, err := demoseedtest.SnapshotClickHouse(ctx, ch, constants.DemoOrganizationID, demoProjects)
	require.NoError(t, err)

	requirePostgresRowsPreserved(t, pgBefore, pgAfter1)
	requireNoLeakIntoOtherTenants(t, addedRows(pgBefore, pgAfter1), fixtureUUIDs)
	requireClickHouseOutsideUnchanged(t, chBefore, chAfter1)

	// Plant stray demo-scope rows: the next run must clean them up, restoring
	// the exact per-table counts of the first run.
	require.NoError(t, demoseedtest.TamperDemoRows(ctx, db, ch, constants.DemoOrganizationID, DefaultSpec().ProjectID()))

	// Second real seed run.
	require.NoError(t, Run(ctx, logger, db, ch, blob, DefaultSpec()))

	pgAfter2, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)
	chAfter2, err := demoseedtest.SnapshotClickHouse(ctx, ch, constants.DemoOrganizationID, demoProjects)
	require.NoError(t, err)

	requirePostgresRowsPreserved(t, pgBefore, pgAfter2)
	requireNoLeakIntoOtherTenants(t, addedRows(pgBefore, pgAfter2), fixtureUUIDs)
	requireClickHouseOutsideUnchanged(t, chBefore, chAfter2)

	// Idempotence + cleanup: every Postgres table holds exactly as many rows
	// as after the first run (the tampered chat included would be +1).
	for table, hashes := range pgAfter1 {
		require.Equal(t, totalRows(hashes), totalRows(pgAfter2[table]),
			"postgres table %s: row count changed between seed runs — reseed is not cleaning up or not idempotent", table)
	}
	for table := range pgAfter2 {
		_, ok := pgAfter1[table]
		require.True(t, ok, "postgres table %s appeared between seed runs", table)
	}

	// ClickHouse: the seed writes deterministic row counts into plain
	// MergeTree tables. Summing/Aggregating MV targets collapse rows by
	// time-bucketed keys that shift with now(), so only their isolation is
	// asserted (above), not exact demo-scope counts.
	for table, s1 := range chAfter1 {
		s2 := chAfter2[table]
		if isPlainMergeTree(s1.Engine) {
			require.Equal(t, s1.DemoCount, s2.DemoCount,
				"clickhouse table %s: demo row count changed between seed runs — reseed is not cleaning up or not idempotent", table)
		}
	}
}

// isPlainMergeTree reports whether the engine stores rows verbatim (including
// the Replicated variant), as opposed to the Summing/Aggregating/Replacing/
// Collapsing family members that merge same-key rows and so cannot promise
// stable row counts.
func isPlainMergeTree(engine string) bool {
	return engine == "MergeTree" || engine == "ReplicatedMergeTree"
}

func requirePostgresRowsPreserved(t *testing.T, before, after demoseedtest.PostgresSnapshot) {
	t.Helper()

	for table, hashes := range before {
		for hash, n := range hashes {
			require.GreaterOrEqual(t, after[table][hash], n,
				"postgres table %s: a pre-existing row of another tenant was modified or deleted by the demo seed", table)
		}
	}
}

func requireClickHouseOutsideUnchanged(t *testing.T, before, after map[string]demoseedtest.ClickHouseTableState) {
	t.Helper()

	for table, b := range before {
		a := after[table]
		require.Equal(t, b.OutsideCount, a.OutsideCount,
			"clickhouse table %s: non-demo row count changed — the demo seed touched another tenant's rows", table)
		require.Equal(t, b.OutsideHash, a.OutsideHash,
			"clickhouse table %s: non-demo row content changed — the demo seed touched another tenant's rows", table)
	}
}

// addedRows returns, per table, the rows present in after beyond their
// occurrence count in before.
func addedRows(before, after demoseedtest.PostgresSnapshot) map[string][]string {
	added := map[string][]string{}
	for table, rows := range after {
		for row, n := range rows {
			if extra := n - before[table][row]; extra > 0 {
				added[table] = append(added[table], row)
			}
		}
	}
	return added
}

var uuidRe = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func collectUUIDs(rowsByTable map[string][]string) map[string]bool {
	uuids := map[string]bool{}
	for _, rows := range rowsByTable {
		for _, row := range rows {
			for _, u := range uuidRe.FindAllString(row, -1) {
				uuids[u] = true
			}
		}
	}
	return uuids
}

// requireNoLeakIntoOtherTenants asserts that no row the seed run added
// references the fixture tenant — neither by its marker identifiers (org id,
// user ids, emails, ...) nor by any uuid that appears in the fixture's rows
// (project ids, chat ids, foreign keys). Together with the preserved-rows
// check this proves the seed neither rewrites other tenants' rows nor attaches
// new rows to them.
func requireNoLeakIntoOtherTenants(t *testing.T, added map[string][]string, fixtureUUIDs map[string]bool) {
	t.Helper()

	for table, rows := range added {
		for _, row := range rows {
			for _, id := range otherTenantSpec.Identifiers() {
				require.NotContains(t, row, id,
					"postgres table %s: a row added by the demo seed references the other tenant's identifier %q: %s", table, id, row)
			}
			for _, u := range uuidRe.FindAllString(row, -1) {
				require.False(t, fixtureUUIDs[u],
					"postgres table %s: a row added by the demo seed references the other tenant's id %s: %s", table, u, row)
			}
		}
	}
}

func totalRows(hashes map[string]int) int {
	n := 0
	for _, c := range hashes {
		n += c
	}
	return n
}
