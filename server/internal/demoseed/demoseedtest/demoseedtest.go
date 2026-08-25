// Package demoseedtest holds the generic database snapshot/exec helpers behind
// TestDemoSeedSafety. They live outside the _test.go files because the safety
// test is deliberately schema-agnostic — it snapshots EVERY table via
// information_schema/system.tables with dynamically built SQL, which cannot be
// expressed through SQLc queries (and the glint no-testing-raw-sql rule bans
// raw SQL in _test.go files for exactly the fixture-writing cases that SQLc
// does cover).
package demoseedtest

import (
	"context"
	"fmt"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExecPostgresScript applies a multi-statement SQL script using the simple
// query protocol, mirroring how demoseed.Run applies the real seed.
func ExecPostgresScript(ctx context.Context, db *pgxpool.Pool, script string) error {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire postgres connection: %w", err)
	}
	defer conn.Release()

	if err := conn.Conn().PgConn().Exec(ctx, script).Close(); err != nil {
		return fmt.Errorf("exec postgres script: %w", err)
	}
	return nil
}

// ExecClickHouseStatements runs pre-split statements one by one, skipping SET
// lines, mirroring how demoseed.Run applies the ClickHouse seed.
func ExecClickHouseStatements(ctx context.Context, ch driver.Conn, stmts []string) error {
	for _, stmt := range stmts {
		if strings.HasPrefix(strings.ToUpper(stmt), "SET ") {
			continue
		}
		if err := ch.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec clickhouse statement: %w: %.120s", err, stmt)
		}
	}
	return nil
}

// PostgresSnapshot is a per-table multiset of full row fingerprints:
// table name -> row JSON -> occurrence count. Full row text (not a digest) is
// kept so tests can inspect rows that appear between snapshots.
type PostgresSnapshot map[string]map[string]int

// SnapshotPostgres fingerprints every row of every public base table. Row
// identity is the record's JSON representation, so ANY column change, delete,
// or insert is visible in the multiset.
func SnapshotPostgres(ctx context.Context, db *pgxpool.Pool) (PostgresSnapshot, error) {
	rows, err := db.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		ORDER BY table_name`)
	if err != nil {
		return nil, fmt.Errorf("list postgres tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan postgres table name: %w", err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres tables: %w", err)
	}

	snap := make(PostgresSnapshot, len(tables))
	for _, table := range tables {
		hashes := map[string]int{}
		q := fmt.Sprintf(`SELECT to_jsonb(t)::text FROM %s t`, pgx.Identifier{"public", table}.Sanitize())
		rows, err := db.Query(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("hash rows of %s: %w", table, err)
		}
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				return nil, fmt.Errorf("scan row hash of %s: %w", table, err)
			}
			hashes[h]++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate row hashes of %s: %w", table, err)
		}
		snap[table] = hashes
	}
	return snap, nil
}

// ClickHouseTableState captures one table's demo/non-demo split. Rows are
// classified by the table's organization_id / gram_project_id columns; tables
// with neither column treat every row as outside the demo scope.
type ClickHouseTableState struct {
	Engine string
	// OutsideCount/OutsideHash cover rows NOT belonging to the demo org or
	// demo projects. The hash is an order-independent sum of per-row
	// cityHash64 over all stringifiable columns.
	OutsideCount uint64
	OutsideHash  uint64
	// DemoCount covers rows in the demo scope.
	DemoCount uint64
}

// SnapshotClickHouse captures the state of every MergeTree-family table in the
// current database. Each table is OPTIMIZE ... FINAL'd first so background
// merges (which collapse rows in Summing/Aggregating tables) cannot shift
// counts between two snapshots.
func SnapshotClickHouse(ctx context.Context, ch driver.Conn, orgID string, projectIDs []string) (map[string]ClickHouseTableState, error) {
	rows, err := ch.Query(ctx, `
		SELECT name, engine FROM system.tables
		WHERE database = currentDatabase() AND engine LIKE '%MergeTree%'
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list clickhouse tables: %w", err)
	}
	type tbl struct{ name, engine string }
	var tables []tbl
	for rows.Next() {
		var t tbl
		if err := rows.Scan(&t.name, &t.engine); err != nil {
			return nil, fmt.Errorf("scan clickhouse table: %w", err)
		}
		tables = append(tables, t)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close clickhouse tables cursor: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clickhouse tables: %w", err)
	}

	quotedProjects := make([]string, len(projectIDs))
	for i, p := range projectIDs {
		quotedProjects[i] = fmt.Sprintf("toUUID('%s')", p)
	}
	projectList := strings.Join(quotedProjects, ", ")

	snap := make(map[string]ClickHouseTableState, len(tables))
	for _, t := range tables {
		if err := ch.Exec(ctx, fmt.Sprintf("OPTIMIZE TABLE `%s` FINAL", t.name)); err != nil {
			return nil, fmt.Errorf("optimize %s: %w", t.name, err)
		}

		cols, err := ch.Query(ctx, `
			SELECT name, type, default_kind FROM system.columns
			WHERE database = currentDatabase() AND table = $1
			ORDER BY position`, t.name)
		if err != nil {
			return nil, fmt.Errorf("list columns of %s: %w", t.name, err)
		}
		var hashParts []string
		var demoPreds []string
		for cols.Next() {
			var name, typ, defaultKind string
			if err := cols.Scan(&name, &typ, &defaultKind); err != nil {
				return nil, fmt.Errorf("scan column of %s: %w", t.name, err)
			}
			// Skipped from the hash:
			//   - AggregateFunction states (no stable string form);
			//   - array-valued SimpleAggregateFunction columns
			//     (groupUniqArrayArray & co. — element order is not
			//     deterministic across part merges);
			//   - MATERIALIZED/ALIAS columns (derived, not part of SELECT *).
			// Rows stay covered by their stored scalar columns and the count.
			hashable := !strings.HasPrefix(typ, "AggregateFunction") &&
				(!strings.HasPrefix(typ, "SimpleAggregateFunction") || !strings.Contains(typ, "Array")) &&
				(defaultKind == "" || defaultKind == "DEFAULT")
			if hashable {
				hashParts = append(hashParts, fmt.Sprintf("ifNull(toString(`%s`), '<null>')", name))
			}
			switch name {
			case "organization_id":
				demoPreds = append(demoPreds, fmt.Sprintf("organization_id = '%s'", orgID))
			case "gram_project_id":
				demoPreds = append(demoPreds, fmt.Sprintf("gram_project_id IN (%s)", projectList))
			}
		}
		if err := cols.Close(); err != nil {
			return nil, fmt.Errorf("close columns cursor of %s: %w", t.name, err)
		}
		if err := cols.Err(); err != nil {
			return nil, fmt.Errorf("iterate columns of %s: %w", t.name, err)
		}

		demoPred := "0" // no scoping column: nothing in this table is demo data
		if len(demoPreds) > 0 {
			demoPred = strings.Join(demoPreds, " OR ")
		}
		// cityHash64 needs at least one argument; a table whose every column
		// is excluded above still gets counted, just with a constant hash.
		if len(hashParts) == 0 {
			hashParts = []string{"''"}
		}

		q := fmt.Sprintf(`
			SELECT
				countIf(NOT inDemo),
				sumIf(cityHash64(%s), NOT inDemo),
				countIf(inDemo)
			FROM (SELECT *, (%s) AS inDemo FROM `+"`%s`"+`)`,
			strings.Join(hashParts, ", "), demoPred, t.name)

		var state ClickHouseTableState
		state.Engine = t.engine
		if err := ch.QueryRow(ctx, q).Scan(&state.OutsideCount, &state.OutsideHash, &state.DemoCount); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", t.name, err)
		}
		snap[t.name] = state
	}
	return snap, nil
}

// TamperDemoRows plants stray rows inside the demo scope — a duplicated chat
// and a visitor-minted API key in Postgres, a duplicated telemetry row in
// ClickHouse — so a subsequent seed run can be shown to clean up unexpected
// demo-org data, not merely re-assert its own rows.
func TamperDemoRows(ctx context.Context, db *pgxpool.Pool, ch driver.Conn, orgID string, projectID string) error {
	// Generated columns (e.g. chats.deleted) cannot be written, so the
	// duplicated row is built column by column from the non-generated set.
	colRows, err := db.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'chats' AND is_generated = 'NEVER'
		ORDER BY ordinal_position`)
	if err != nil {
		return fmt.Errorf("list chats columns: %w", err)
	}
	var cols []string
	for colRows.Next() {
		var name string
		if err := colRows.Scan(&name); err != nil {
			return fmt.Errorf("scan chats column: %w", err)
		}
		cols = append(cols, pgx.Identifier{name}.Sanitize())
	}
	colRows.Close()
	if err := colRows.Err(); err != nil {
		return fmt.Errorf("iterate chats columns: %w", err)
	}
	colList := strings.Join(cols, ", ")

	tag, err := db.Exec(ctx, fmt.Sprintf(`
		INSERT INTO chats (%s)
		SELECT %s FROM jsonb_populate_record(
			NULL::chats,
			(SELECT to_jsonb(c) || jsonb_build_object('id', gen_random_uuid())
			 FROM chats c WHERE organization_id = $1 LIMIT 1))`, colList, colList), orgID)
	if err != nil {
		return fmt.Errorf("tamper postgres chat: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("tamper postgres chat: expected 1 row inserted, got %d", tag.RowsAffected())
	}

	// The seed itself never inserts an API key or a LiteLLM instance, so these
	// rows cannot be cloned from existing ones: they stand in for what a demo
	// visitor creates with the org:admin grants every demo session holds. The
	// projects delete only SET NULLs api_keys.project_id, so the key needs an
	// explicit scoped delete — and litellm_instances.api_key_id is ON DELETE
	// RESTRICT, so the instance must be deleted before the key or the whole
	// reseed aborts on the FK.
	if _, err := db.Exec(ctx, `
		WITH key AS (
			INSERT INTO api_keys (organization_id, project_id, created_by_user_id, name, key_prefix, key_hash, scopes)
			VALUES ($1, $2::uuid, 'user_demo_tamper', 'tampered visitor key', 'gram_demo', 'DEMO-TAMPER-HASH', ARRAY['producer'])
			RETURNING organization_id, project_id, id
		)
		INSERT INTO litellm_instances (organization_id, project_id, api_key_id, created_by_user_id, name)
		SELECT organization_id, project_id, id, 'user_demo_tamper', 'tampered visitor instance' FROM key`,
		orgID, projectID,
	); err != nil {
		return fmt.Errorf("tamper postgres api key: %w", err)
	}

	err = ch.Exec(ctx, fmt.Sprintf(`
		INSERT INTO telemetry_logs
		SELECT * FROM telemetry_logs WHERE gram_project_id = toUUID('%s') LIMIT 1`, projectID))
	if err != nil {
		return fmt.Errorf("tamper clickhouse telemetry row: %w", err)
	}
	return nil
}
