//nolint:glint // Schema regression tests exercise constraints with raw SQL.
package database_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

const emaScopeFixtures = `
INSERT INTO organization_metadata (id, name, slug) VALUES
 ('fixture-org-a', 'Fixture A', 'fixture-a'), ('fixture-org-b', 'Fixture B', 'fixture-b');
INSERT INTO projects (id, name, slug, organization_id)
SELECT ('00000000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid,
 'Fixture ' || n, 'fixture-' || n, CASE WHEN n = 3 THEN 'fixture-org-b' ELSE 'fixture-org-a' END
FROM generate_series(1, 3) n;
CREATE FUNCTION fixture_scope_id(n integer) RETURNS uuid LANGUAGE sql AS $$
 SELECT ('00000000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid;
$$;
INSERT INTO user_session_issuers (id, project_id, organization_id, slug, authn_challenge_mode, session_duration)
SELECT fixture_scope_id(n), CASE WHEN n <= 3 THEN fixture_scope_id(n) END,
 CASE WHEN n IN (1,2,4) THEN 'fixture-org-a' WHEN n IN (3,5) THEN 'fixture-org-b' END,
 'fixture-' || n, 'chain', interval '1 hour' FROM generate_series(1,6) n;
INSERT INTO remote_session_issuers (id, project_id, organization_id, slug, issuer)
SELECT fixture_scope_id(n), CASE WHEN n <= 3 THEN fixture_scope_id(n) END,
 CASE WHEN n IN (1,2,4) THEN 'fixture-org-a' WHEN n IN (3,5) THEN 'fixture-org-b' END,
 'fixture-' || n, 'https://issuer.example.invalid/' || n FROM generate_series(1,6) n;
UPDATE user_session_issuers SET trusted_remote_session_issuer_id = fixture_scope_id(6);
INSERT INTO remote_session_clients (id, project_id, organization_id, remote_session_issuer_id, client_id)
SELECT fixture_scope_id(n), CASE WHEN n <= 3 THEN fixture_scope_id(n) END,
 CASE WHEN n IN (1,2,4) THEN 'fixture-org-a' WHEN n IN (3,5) THEN 'fixture-org-b' END,
 fixture_scope_id(6), 'fixture-' || n FROM generate_series(1,6) n;
`

const emaScopeInsert = `INSERT INTO remote_session_ema_bindings
 (project_id, organization_id, user_session_issuer_id, remote_session_issuer_id, remote_session_client_id, resource)
 VALUES (fixture_scope_id(1), 'fixture-org-a', fixture_scope_id(1), fixture_scope_id(6), fixture_scope_id(1), 'https://resource.example.invalid')`

func requireEMAScopeError(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23503", pgErr.Code)
}

//nolint:paralleltest,tparallel // Subtests mutate shared fixture rows and must run serially; only the parent runs in parallel.
func TestEMABindingScope(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	container, clone, err := testenv.NewTestPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	pool, err := clone(t, "ema_scope")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, emaScopeFixtures)
	require.NoError(t, err)

	// Project / organization user issuers; project / organization / global
	// remote issuers. Client-less in-progress bindings are valid at every tier.
	for u := 1; u <= 6; u++ {
		for r := 1; r <= 6; r++ {
			t.Run(fmt.Sprintf("issuers_%d_%d", u, r), func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback(ctx) }()
				_, err = tx.Exec(ctx, `UPDATE user_session_issuers SET trusted_remote_session_issuer_id = fixture_scope_id($2) WHERE id = fixture_scope_id($1)`, u, r)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, `INSERT INTO remote_session_ema_bindings
 (project_id, organization_id, user_session_issuer_id, remote_session_issuer_id, resource)
 VALUES (fixture_scope_id(1), 'fixture-org-a', fixture_scope_id($1), fixture_scope_id($2), 'https://resource.example.invalid')`, u, r)
				if (u == 1 || u == 4) && (r == 1 || r == 4 || r == 6) {
					require.NoError(t, err)
				} else {
					requireEMAScopeError(t, err)
				}
			})
		}
	}
	for c := 1; c <= 6; c++ {
		t.Run(fmt.Sprintf("client_%d", c), func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, emaScopeInsert)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `UPDATE remote_session_ema_bindings SET remote_session_client_id = fixture_scope_id($1)`, c)
			if c == 1 || c == 4 {
				require.NoError(t, err)
			} else {
				requireEMAScopeError(t, err)
			}
		})
	}
	for name, mutation := range map[string]string{
		"user_issuer":          "user_session_issuer_id = fixture_scope_id(2)",
		"remote_issuer":        "remote_session_issuer_id = fixture_scope_id(2)",
		"client_issuer_pair":   "remote_session_issuer_id = fixture_scope_id(1)",
		"project_organization": "organization_id = 'fixture-org-b'",
	} {
		t.Run("retarget_"+name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, emaScopeInsert)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "UPDATE remote_session_ema_bindings SET "+mutation)
			requireEMAScopeError(t, err)
		})
	}
	for name, trusted := range map[string]string{"untrusted_sibling": "fixture_scope_id(4)", "no_trusted_issuer": "NULL"} {
		t.Run(name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, "UPDATE user_session_issuers SET trusted_remote_session_issuer_id = "+trusted+" WHERE id = fixture_scope_id(1)")
			require.NoError(t, err)
			_, err = tx.Exec(ctx, emaScopeInsert)
			requireEMAScopeError(t, err)
		})
	}
	for _, field := range []string{"authorization_endpoint", "token_endpoint", "revocation_endpoint", "registration_endpoint", "jwks_uri", "userinfo_endpoint", "introspection_endpoint", "tunneled_mcp_server_id"} {
		t.Run(field+"_requires_unlink", func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, emaScopeInsert)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "UPDATE remote_session_issuers SET "+field+" = "+field+" WHERE id = fixture_scope_id(6)")
			require.NoError(t, err)
			value := "'https://changed.example.invalid'"
			if field == "tunneled_mcp_server_id" {
				value = "fixture_scope_id(99)"
			}
			_, err = tx.Exec(ctx, "UPDATE remote_session_issuers SET "+field+" = "+value+" WHERE id = fixture_scope_id(6)")
			requireEMAScopeError(t, err)
			require.ErrorContains(t, err, "active identity-chaining binding must be explicitly unlinked")
		})
	}
	for field, value := range map[string]string{"scope_override": "ARRAY['fixture:read']", "resource_indicator_supported": "FALSE"} {
		t.Run(field+"_requires_unlink", func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, emaScopeInsert)
			require.NoError(t, err)
			mutation := "UPDATE remote_session_issuers SET " + field + " = " + value + " WHERE id = fixture_scope_id(6)"
			noop := "UPDATE remote_session_issuers SET " + field + " = " + field + " WHERE id = fixture_scope_id(6)"
			_, err = tx.Exec(ctx, noop)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "SAVEPOINT reconfigure")
			require.NoError(t, err)
			_, err = tx.Exec(ctx, mutation)
			requireEMAScopeError(t, err)
			require.ErrorContains(t, err, "active identity-chaining binding must be explicitly unlinked")
			_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT reconfigure; UPDATE remote_session_ema_bindings SET state = 'unlinked', generation = generation + 1")
			require.NoError(t, err)
			_, err = tx.Exec(ctx, mutation)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "UPDATE remote_session_ema_bindings SET state = 'ready', generation = generation + 1")
			require.NoError(t, err)
			// No-op refreshes remain valid when the pinned value is non-NULL.
			_, err = tx.Exec(ctx, noop)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "UPDATE remote_session_issuers SET "+field+" = NULL WHERE id = fixture_scope_id(6)")
			requireEMAScopeError(t, err)
		})
	}
	for _, generation := range []int{0, 1, 3} {
		t.Run(fmt.Sprintf("unlink_rejects_generation_%d", generation), func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, emaScopeInsert)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "UPDATE remote_session_ema_bindings SET state = 'unlinked', generation = $1", generation)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23514", pgErr.Code)
		})
	}
	t.Run("secret_expiry_requires_unlink", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `UPDATE remote_session_clients SET client_secret_expires_at = '2100-01-01' WHERE id = fixture_scope_id(1)`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, emaScopeInsert)
		require.NoError(t, err)
		// A no-op assignment does not reconfigure the credential.
		_, err = tx.Exec(ctx, `UPDATE remote_session_clients SET client_secret_expires_at = client_secret_expires_at WHERE id = fixture_scope_id(1)`)
		require.NoError(t, err)
		for _, expiry := range []string{"NULL", "'2000-01-01'", "'2101-01-01'"} {
			_, err = tx.Exec(ctx, "SAVEPOINT expiry")
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "UPDATE remote_session_clients SET client_secret_expires_at = "+expiry+" WHERE id = fixture_scope_id(1)")
			requireEMAScopeError(t, err)
			_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT expiry")
			require.NoError(t, err)
		}
		_, err = tx.Exec(ctx, `UPDATE remote_session_ema_bindings SET state = 'unlinked', generation = generation + 1;
UPDATE remote_session_clients SET client_secret_expires_at = '2000-01-01' WHERE id = fixture_scope_id(1)`)
		require.NoError(t, err)
	})

	t.Run("revival_requires_next_generation", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, emaScopeInsert)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, "UPDATE remote_session_ema_bindings SET state = 'unlinked', generation = 2")
		require.NoError(t, err)
		for _, mutation := range []string{"state = 'ready'", "state = 'ready', generation = 4", "generation = 1"} {
			_, err = tx.Exec(ctx, "SAVEPOINT revival")
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "UPDATE remote_session_ema_bindings SET "+mutation)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23514", pgErr.Code)
			_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT revival")
			require.NoError(t, err)
		}
	})

	t.Run("generation_is_binding_incarnation", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, emaScopeInsert)
		require.NoError(t, err)
		// Claim selection advances the incarnation. Status/provenance completion
		// belongs to that claim, not to a new binding incarnation.
		selected, err := tx.Exec(ctx, `UPDATE remote_session_ema_bindings SET generation = 2,
 state = 'in_progress', remote_session_client_id = NULL, claim_id = fixture_scope_id(100), claimed_at = clock_timestamp()
 WHERE project_id = fixture_scope_id(1) AND organization_id = 'fixture-org-a' AND generation = 1`)
		require.NoError(t, err)
		require.EqualValues(t, 1, selected.RowsAffected())
		complete := `UPDATE remote_session_ema_bindings SET state = 'ready', grant_source = 'provider_returned',
 remote_session_client_id = fixture_scope_id(1), claim_id = NULL, claimed_at = NULL
 WHERE project_id = fixture_scope_id(1) AND organization_id = 'fixture-org-a'
 AND generation = $1 AND claim_id = fixture_scope_id($2) AND state = 'in_progress'`
		for _, stale := range [][2]int{{1, 100}, {2, 101}} {
			result, err := tx.Exec(ctx, complete, stale[0], stale[1])
			require.NoError(t, err)
			require.Zero(t, result.RowsAffected())
		}
		result, err := tx.Exec(ctx, complete, 2, 100)
		require.NoError(t, err)
		require.EqualValues(t, 1, result.RowsAffected())
		var generation int64
		err = tx.QueryRow(ctx, `SELECT generation FROM remote_session_ema_bindings`).Scan(&generation)
		require.NoError(t, err)
		require.EqualValues(t, 2, generation)
		// The cleared claim prevents replay even though generation is unchanged.
		result, err = tx.Exec(ctx, complete, 2, 100)
		require.NoError(t, err)
		require.Zero(t, result.RowsAffected())
		result, err = tx.Exec(ctx, `UPDATE remote_session_ema_bindings SET state = 'unlinked', generation = 3
 WHERE project_id = fixture_scope_id(1) AND organization_id = 'fixture-org-a' AND generation = 2`)
		require.NoError(t, err)
		require.EqualValues(t, 1, result.RowsAffected())
		// A stale expected-generation update cannot restore the old incarnation.
		result, err = tx.Exec(ctx, `UPDATE remote_session_ema_bindings SET state = 'ready'
 WHERE project_id = fixture_scope_id(1) AND organization_id = 'fixture-org-a' AND generation = 2`)
		require.NoError(t, err)
		require.Zero(t, result.RowsAffected())
		result, err = tx.Exec(ctx, `UPDATE remote_session_ema_bindings SET state = 'ready', generation = 4,
 remote_session_client_id = fixture_scope_id(4), grant_source = 'administrator_declared'
 WHERE project_id = fixture_scope_id(1) AND organization_id = 'fixture-org-a' AND generation = 3`)
		require.NoError(t, err)
		require.EqualValues(t, 1, result.RowsAffected())
	})

	t.Run("tombstone_and_grant_evidence", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, emaScopeInsert)
		require.NoError(t, err)
		// Evidence publication is not a credential/configuration mutation.
		_, err = tx.Exec(ctx, `UPDATE remote_session_clients SET grant_types = ARRAY['urn:ietf:params:oauth:grant-type:jwt-bearer'] WHERE id = fixture_scope_id(1)`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `UPDATE remote_session_ema_bindings SET state = 'unlinked', generation = generation + 1;
UPDATE projects SET organization_id = 'fixture-org-b' WHERE id = fixture_scope_id(1);
UPDATE user_session_issuers SET project_id = fixture_scope_id(2) WHERE id = fixture_scope_id(1);
UPDATE remote_session_ema_bindings SET updated_at = clock_timestamp()`)
		require.NoError(t, err)
		var generation int64
		var org string
		err = tx.QueryRow(ctx, `SELECT organization_id, generation FROM remote_session_ema_bindings`).Scan(&org, &generation)
		require.NoError(t, err)
		require.Equal(t, "fixture-org-b", org)
		require.EqualValues(t, 2, generation)
		_, err = tx.Exec(ctx, "SAVEPOINT revive")
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `UPDATE remote_session_ema_bindings SET state = 'ready', generation = generation + 1`)
		requireEMAScopeError(t, err)
		_, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT revive; DELETE FROM projects WHERE id = fixture_scope_id(1)`)
		require.NoError(t, err)
		var count int
		err = tx.QueryRow(ctx, `SELECT count(*) FROM remote_session_ema_bindings`).Scan(&count)
		require.NoError(t, err)
		require.Zero(t, count)
	})

	t.Run("foreign_key_lookup_indexes", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, emaScopeInsert)
		require.NoError(t, err)
		// Populate distinct projects and clients so the normal planner (without
		// disabling sequential scans) can demonstrate both selective prefixes.
		_, err = tx.Exec(ctx, `
INSERT INTO projects (id, name, slug, organization_id)
SELECT fixture_scope_id(n), 'Index fixture', 'index-' || n, 'fixture-org-a' FROM generate_series(100, 5099) n;
INSERT INTO remote_session_clients (id, organization_id, remote_session_issuer_id, client_id)
SELECT fixture_scope_id(n), 'fixture-org-a', fixture_scope_id(6), 'index-' || n FROM generate_series(100, 5099) n;
INSERT INTO remote_session_ema_bindings
 (project_id, organization_id, user_session_issuer_id, remote_session_issuer_id, remote_session_client_id, resource)
SELECT fixture_scope_id(n), 'fixture-org-a', fixture_scope_id(4), fixture_scope_id(6), fixture_scope_id(n),
 'https://resource.example.invalid' FROM generate_series(100, 5099) n;
ANALYZE remote_session_ema_bindings;`)
		require.NoError(t, err)
		for _, tc := range []struct{ predicate, index string }{
			{"organization_id = 'fixture-org-a' AND project_id = fixture_scope_id(1)", "remote_session_ema_bindings_resource_key"},
			{"remote_session_client_id = fixture_scope_id(1) AND remote_session_issuer_id = fixture_scope_id(6)", "remote_session_ema_bindings_client_idx"},
		} {
			var plan string
			err = tx.QueryRow(ctx, "EXPLAIN (FORMAT JSON) SELECT 1 FROM remote_session_ema_bindings WHERE "+tc.predicate).Scan(&plan)
			require.NoError(t, err)
			require.Contains(t, plan, tc.index)
			require.NotContains(t, plan, "Seq Scan")
		}
	})

	// Exercise both race orderings against every locked parent, including the
	// first binding (where a parent guard initially has no child row to find).
	for name, mutation := range map[string]string{
		"project":       "UPDATE projects SET organization_id = 'fixture-org-b' WHERE id = fixture_scope_id(1)",
		"user_issuer":   "UPDATE user_session_issuers SET project_id = fixture_scope_id(2) WHERE id = fixture_scope_id(1)",
		"remote_issuer": "UPDATE remote_session_issuers SET project_id = fixture_scope_id(2) WHERE id = fixture_scope_id(6)",
		"client":        "UPDATE remote_session_clients SET project_id = fixture_scope_id(2) WHERE id = fixture_scope_id(1)",
	} {
		for _, parentFirst := range []bool{true, false} {
			t.Run(fmt.Sprintf("race_%s_parent_first_%t", name, parentFirst), func(t *testing.T) {
				pool, err := clone(t, fmt.Sprintf("ema_%s_%t", name, parentFirst))
				require.NoError(t, err)
				_, err = pool.Exec(ctx, emaScopeFixtures)
				require.NoError(t, err)
				first, second := emaScopeInsert, mutation
				if parentFirst {
					first, second = second, first
				}
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback(ctx) }()
				_, err = tx.Exec(ctx, first)
				require.NoError(t, err)
				conn, err := pool.Acquire(ctx)
				require.NoError(t, err)
				defer conn.Release()
				done := make(chan error, 1)
				waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				go func() { _, err := conn.Exec(waitCtx, second); done <- err }()
				waitForEMAScopeLock(t, pool, conn.Conn().PgConn().PID())
				require.NoError(t, tx.Commit(ctx))
				requireEMAScopeError(t, <-done)
			})
		}
	}
}

func waitForEMAScopeLock(t *testing.T, pool *pgxpool.Pool, pid uint32) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid = $1 AND wait_event_type = 'Lock')`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
}
