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

	if err := PlantMCPServerDependents(ctx, db, orgID, projectID); err != nil {
		return fmt.Errorf("tamper postgres MCP server dependents: %w", err)
	}

	err = ch.Exec(ctx, fmt.Sprintf(`
		INSERT INTO telemetry_logs
		SELECT * FROM telemetry_logs WHERE gram_project_id = toUUID('%s') LIMIT 1`, projectID))
	if err != nil {
		return fmt.Errorf("tamper clickhouse telemetry row: %w", err)
	}
	return nil
}

// PlantMCPServerDependents adds realistic tenant-owned rows across the full
// catalog-registration FK closure. Every insert is gated on target_server so a
// project without an MCP server cannot retain unattached fixture parents.
func PlantMCPServerDependents(ctx context.Context, db *pgxpool.Pool, orgID, projectID string) error {
	tag, err := db.Exec(ctx, `
		WITH target_server AS MATERIALIZED (
			SELECT id FROM mcp_servers WHERE project_id = $2::uuid ORDER BY id LIMIT 1
		), new_assistant AS (
			INSERT INTO assistants (project_id, organization_id, name, model, instructions)
			SELECT $2::uuid, $1, 'Reseed safety assistant', 'openai/gpt-4o-mini', 'Test reseed cleanup.'
			FROM target_server
			RETURNING id
		), assistant_attachment AS (
			INSERT INTO assistant_mcp_servers (assistant_id, mcp_server_id, project_id)
			SELECT new_assistant.id, target_server.id, $2::uuid
			FROM new_assistant CROSS JOIN target_server
			RETURNING id
		), new_plugin AS (
			INSERT INTO plugins (organization_id, project_id, name, slug)
			SELECT $1, $2::uuid, 'Reseed safety plugin', 'reseed-safety-plugin'
			FROM target_server
			RETURNING id
		), plugin_attachment AS (
			INSERT INTO plugin_servers (plugin_id, mcp_server_id, display_name)
			SELECT new_plugin.id, target_server.id, 'Reseed safety server'
			FROM new_plugin CROSS JOIN target_server
			RETURNING id
		), registration AS (
			INSERT INTO platform_mcp_catalog_registrations (
				organization_id, project_id, source_kind, catalog_provider,
				catalog_reference, status, mcp_server_id, acting_surface
			)
			SELECT $1, $2::uuid, 'catalog', 'reseed-safety',
				'reseed-safety-server', 'ready', target_server.id, 'dashboard'
			FROM target_server
			RETURNING id
		), receipt AS (
			INSERT INTO platform_mcp_operation_receipts (
				organization_id, project_id, registration_id, user_id, acting_surface,
				operation, idempotency_key, input_hash, status, expires_at
			)
			SELECT $1, $2::uuid, registration.id, 'user_reseed_safety', 'dashboard',
				'install', 'reseed-safety-install', 'reseed-safety-input', 'completed',
				clock_timestamp() + interval '1 day'
			FROM registration
			RETURNING id
		), workflow AS (
			INSERT INTO platform_mcp_onboarding_workflows (
				organization_id, initiating_subject_urn, source_surface, client_family,
				selected_project_id, selected_registration_id, expires_at
			)
			SELECT $1, 'user:user_reseed_safety', 'dashboard', 'claude',
				$2::uuid, registration.id, clock_timestamp() + interval '1 day'
			FROM registration
			RETURNING id
		), milestone AS (
			INSERT INTO platform_mcp_onboarding_milestones (
				organization_id, milestone, project_id, mcp_key, attempt_id
			)
			SELECT $1, 'registration_succeeded', $2::uuid,
				'reseed-safety:reseed-safety-server', registration.id
			FROM registration
			RETURNING id
		), distribution AS (
			INSERT INTO platform_mcp_distributions (
				organization_id, project_id, registration_id, default_plugin_id, plugin_id,
				plugin_server_id, state, version, attachment_was_created, publication_state,
				user_id, acting_surface
			)
			SELECT $1, $2::uuid, registration.id, new_plugin.id, new_plugin.id,
				plugin_attachment.id, 'installed', 1, true, 'published',
				'user_reseed_safety', 'dashboard'
			FROM registration CROSS JOIN new_plugin CROSS JOIN plugin_attachment
			RETURNING id, registration_id, version
		), feedback AS (
			INSERT INTO platform_mcp_feedback (
				organization_id, subject_urn, project_id, workflow_id, category,
				idempotency_key, input_hash, rating, success, expires_at
			)
			SELECT $1, 'user:user_reseed_safety', $2::uuid, workflow.id, 'onboarding',
				'reseed-safety-feedback', 'reseed-safety-feedback-input', 5, true,
				clock_timestamp() + interval '1 day'
			FROM workflow
			RETURNING id
		)
		INSERT INTO platform_mcp_selected_use_evidence (
			organization_id, project_id, registration_id, distribution_id,
			distribution_version, workflow_id, tool_name, tool_category, succeeded_at
		)
		SELECT $1, $2::uuid, distribution.registration_id, distribution.id,
			distribution.version, workflow.id, 'reseed_safety_tool', 'productivity',
			clock_timestamp()
		FROM distribution CROSS JOIN workflow CROSS JOIN receipt CROSS JOIN feedback
			CROSS JOIN assistant_attachment CROSS JOIN milestone`, orgID, projectID)
	if err != nil {
		return fmt.Errorf("plant MCP server dependents: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("plant MCP server dependents: expected 1 complete dependent set, got %d", tag.RowsAffected())
	}
	return nil
}

// CountReseedSafetyProjectMilestones returns the number of project-scoped
// onboarding milestones planted by PlantMCPServerDependents.
func CountReseedSafetyProjectMilestones(ctx context.Context, db *pgxpool.Pool, orgID, projectID string) (int, error) {
	var count int
	err := db.QueryRow(ctx, `
		SELECT count(*)
		FROM platform_mcp_onboarding_milestones
		WHERE organization_id = $1
		  AND project_id = $2::uuid
		  AND milestone = 'registration_succeeded'
		  AND mcp_key = 'reseed-safety:reseed-safety-server'`, orgID, projectID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count reseed safety project milestones: %w", err)
	}
	return count, nil
}

// CreateProjectWithoutMCPServer adds a project used to prove the dependent
// fixture is a no-op when its required target server is absent.
func CreateProjectWithoutMCPServer(ctx context.Context, db *pgxpool.Pool, orgID, projectID string) error {
	tag, err := db.Exec(ctx, `
		INSERT INTO projects (id, organization_id, name, slug)
		VALUES ($2::uuid, $1, 'Reseed safety no-server project', 'reseed-safety-no-server')`,
		orgID, projectID,
	)
	if err != nil {
		return fmt.Errorf("create project without MCP server: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("create project without MCP server: expected 1 project, got %d", tag.RowsAffected())
	}
	return nil
}

// LoginArtifacts is the state a developer's first login leaves in Postgres
// before the local seed has ever run: the organization (created
// un-whitelisted, already linked to WorkOS), the user row — whose id the auth
// callback derives from the IDP subject rather than from the email the seed
// keys on — and the membership tying the two together.
type LoginArtifacts struct {
	OrgID string
	// OrgWorkOSID is the WorkOS organization the callback linked the org to,
	// so the seed meets an already-linked row rather than a bare one.
	OrgWorkOSID string
	OrgName     string
	OrgSlug     string
	UserID      string
	Email       string
	// WorkOSID is the login subject, which the callback also records on the
	// membership.
	WorkOSID string
	// MembershipID is the WorkOS membership id the callback recorded. It is
	// deliberately not the 'devidp_mem_'-prefixed one the seed writes: the
	// seed has to adopt the membership it finds, not only one it made.
	MembershipID string
}

// PlantLoginArtifacts writes what logging in before seeding would have
// written.
func PlantLoginArtifacts(ctx context.Context, db *pgxpool.Pool, a LoginArtifacts) error {
	if _, err := db.Exec(ctx, `
		INSERT INTO organization_metadata (id, name, slug, workos_id, whitelisted)
		VALUES ($1, $2, $3, $4, FALSE)`, a.OrgID, a.OrgName, a.OrgSlug, a.OrgWorkOSID); err != nil {
		return fmt.Errorf("plant organization: %w", err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO users (id, email, display_name, workos_id)
		VALUES ($1, $2, 'Dev', $3)`, a.UserID, a.Email, a.WorkOSID); err != nil {
		return fmt.Errorf("plant user: %w", err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO organization_user_relationships
			(organization_id, user_id, workos_user_id, workos_membership_id)
		VALUES ($1, $2, $3, $4)`, a.OrgID, a.UserID, a.WorkOSID, a.MembershipID); err != nil {
		return fmt.Errorf("plant membership: %w", err)
	}
	return nil
}

// DeveloperState is what the local fixtures are expected to converge on for
// the developer they adopt, however the database got there.
type DeveloperState struct {
	// Users counts the rows holding the developer's email: more than one
	// means a second identity was minted.
	Users int
	// UserID and WorkOSID come from the single row when Users == 1.
	UserID   string
	WorkOSID string
	// Whitelisted is the organization's gate flag, and OrgWorkOSID its link
	// to the IDP.
	Whitelisted bool
	OrgWorkOSID string
	// Memberships and RoleAssignments count the live rows tying UserID to the
	// organization.
	Memberships     int
	RoleAssignments int
}

// ReadDeveloperState collects the rows the local fixtures are responsible for.
func ReadDeveloperState(ctx context.Context, db *pgxpool.Pool, orgID, email string) (DeveloperState, error) {
	var state DeveloperState

	if err := db.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE email = $1`, email).Scan(&state.Users); err != nil {
		return state, fmt.Errorf("count users: %w", err)
	}
	if state.Users != 1 {
		return state, nil
	}

	if err := db.QueryRow(ctx,
		`SELECT id, coalesce(workos_id, '') FROM users WHERE email = $1`, email,
	).Scan(&state.UserID, &state.WorkOSID); err != nil {
		return state, fmt.Errorf("read user: %w", err)
	}
	if err := db.QueryRow(ctx,
		`SELECT whitelisted, coalesce(workos_id, '') FROM organization_metadata WHERE id = $1`, orgID,
	).Scan(&state.Whitelisted, &state.OrgWorkOSID); err != nil {
		return state, fmt.Errorf("read organization: %w", err)
	}
	if err := db.QueryRow(ctx, `
		SELECT count(*) FROM organization_user_relationships
		WHERE organization_id = $1 AND user_id = $2 AND deleted IS FALSE`,
		orgID, state.UserID).Scan(&state.Memberships); err != nil {
		return state, fmt.Errorf("count memberships: %w", err)
	}
	if err := db.QueryRow(ctx, `
		SELECT count(*) FROM organization_role_assignments
		WHERE organization_id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		orgID, state.UserID).Scan(&state.RoleAssignments); err != nil {
		return state, fmt.Errorf("count role assignments: %w", err)
	}
	return state, nil
}
