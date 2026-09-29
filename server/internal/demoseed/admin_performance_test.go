//go:build demoseed_safety

package demoseed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// Opt-in evidence, never part of demo seeding. CloneTestDatabase supplies an
// isolated local database and drops it on cleanup. SQL is read from SQLc output,
// so this measures the shipped statements, not a similar microbenchmark.
func TestAdminUserSearchPerformance(t *testing.T) {
	path := os.Getenv("ADMIN_USERS_PERF_REPORT")
	if path == "" {
		t.Skip("set ADMIN_USERS_PERF_REPORT to record local query plans")
	}
	file, err := os.Create(path)
	require.NoError(t, err)
	defer file.Close()
	write := func(format string, args ...any) {
		_, err := fmt.Fprintf(file, format, args...)
		require.NoError(t, err)
	}
	write("# Admin users: local performance evidence\n\nReproduce from repository root:\n\n```sh\nADMIN_USERS_PERF_REPORT=/tmp/admin-users-performance.md mise run test:server -tags=demoseed_safety ./internal/demoseed -run '^TestAdminUserSearchPerformance$' -count=1 -timeout=8m\n```\n\nThe recorder replaces its output file. Compare the fresh /tmp report with the retained matrix below; preserve this manually written assessment. Relative output paths resolve from server/internal/demoseed.\n\n")
	source, err := parser.ParseFile(token.NewFileSet(), "../admin/repo/queries.sql.go", nil, 0)
	require.NoError(t, err)
	queries := map[string]string{}
	names := []string{"adminListUsers", "adminCountUsers", "adminListUsersOrganizationPreviews", "adminListUserOrganizations", "adminCountUserOrganizations"}
	ast.Inspect(source, func(node ast.Node) bool {
		v, ok := node.(*ast.ValueSpec)
		if ok && len(v.Names) == 1 && slices.Contains(names, v.Names[0].Name) {
			literal := v.Values[0].(*ast.BasicLit)
			queries[v.Names[0].Name], err = strconv.Unquote(literal.Value)
			require.NoError(t, err)
		}
		return true
	})
	require.Len(t, queries, 5)
	write("## Exact prepared statements\n\nGenerated Task 2 SQL; parameter order is unchanged. PostgreSQL infers parameter types from casts.\n\n```sql\n")
	for _, name := range names {
		write("PREPARE %s AS %s;\n", name, queries[name])
	}
	write("```\n\n")
	const profile = `INSERT INTO users(id,email,display_name)
 SELECT 'perf_user_'||i, 'person'||lpad(i::text,6,'0')||'@search.invalid', 'Fictional Person '||i FROM generate_series(1,%d) i;
INSERT INTO organization_metadata(id,name,slug)
 SELECT 'perf_org_'||i, 'Fictional Studio '||lpad(i::text,6,'0'), 'fictional-studio-'||lpad(i::text,6,'0') FROM generate_series(1,%d) i;
INSERT INTO organization_user_relationships(user_id,organization_id)
 SELECT 'perf_user_'||i, 'perf_org_'||((i+j-2)%%%d+1) FROM generate_series(2,%d) i CROSS JOIN LATERAL generate_series(1,i%%4) j;
INSERT INTO organization_user_relationships(user_id,organization_id)
 SELECT 'perf_user_1', 'perf_org_'||i FROM generate_series(1,200) i;
ANALYZE users; ANALYZE organization_metadata; ANALYZE organization_user_relationships;`
	write("## Profile construction\n\nOnly a fresh isolated test clone is written. Normal users have i %% 4 (0–3) memberships; user 1 has exactly 200. All users eligible, no deleted memberships. Fixture safety tests separately cover lifecycle exclusions.\n\n")
	cases := []struct{ name, params string }{
		{"empty", "'{}','{}','{}','{}'"},
		{"selective email / fanout preview", "'{}','{%%person000001@%%}','{}','{}'"},
		{"selective org", "'{}','{}','{%%studio-000199%%}','{}'"},
		{"common bare", "'{}','{}','{}','{%%fictional%%}'"},
		{"no match", "'{}','{}','{}','{%%absentneedle%%}'"},
		{"one character", "'{}','{}','{}','{%%a%%}'"},
		{"two characters", "'{}','{}','{}','{%%st%%}'"},
		{"mixed", "'{}','{%%person%%}','{%%studio%%}','{%%000199%%}'"},
		{"repeated org", "'{}','{}','{%%fictional%%,%%studio%%}','{}'"},
	}
	for _, size := range []int{10000, 100000} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			conn, err := db.Acquire(ctx)
			require.NoError(t, err)
			defer conn.Release()
			_, err = conn.Exec(ctx, "SET statement_timeout='5s'; SET lock_timeout='1s'")
			require.NoError(t, err)
			sql := fmt.Sprintf(profile, size, size/5, size/5, size)
			start := time.Now()
			_, err = conn.Exec(ctx, sql)
			require.NoError(t, err)
			write("### %d users / %d orgs\n\n```sql\n%s\n```\n\nProfile insert + ANALYZE wall time: %s (not a production write benchmark).\n\n", size, size/5, sql, time.Since(start).Round(time.Millisecond))
			var version, settings string
			require.NoError(t, conn.QueryRow(ctx, "SELECT version(), 'jit='||current_setting('jit')||', shared_buffers='||current_setting('shared_buffers')||', work_mem='||current_setting('work_mem')||', plan_cache_mode='||current_setting('plan_cache_mode')").Scan(&version, &settings))
			var members int
			require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM organization_user_relationships").Scan(&members))
			write("Environment: %s; %s. Memberships: %d. Statement timeout 1500 ms; lock timeout 1 s. Warm sequential runs, default planner/cache settings; three successful runs required for a median.\n\n", version, settings, members)
			_, err = conn.Exec(ctx, "SET statement_timeout='1500ms'")
			require.NoError(t, err)
			for _, name := range names {
				_, err = conn.Exec(ctx, "PREPARE "+name+" AS "+queries[name])
				require.NoError(t, err)
			}
			var slowestPlan string
			var slowestTime float64
			incomplete := []string{}
			// A timeout is recorded once, never silently discarded from a median.
			measure := func(label, exec string) (float64, bool) {
				times := []float64{}
				var last []map[string]any
				for range 3 {
					var data []byte
					err := conn.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+exec).Scan(&data)
					if err != nil {
						if !adminPerfStatementTimeout(err) {
							require.NoError(t, err, "unexpected EXPLAIN error: %s", exec)
						}
						incomplete = append(incomplete, label)
						write("| %s | `%s` | INCOMPLETE: PostgreSQL statement timeout: %s | — | — | — |\n", label, exec, strings.ReplaceAll(err.Error(), "|", "/"))
						return 0, false
					}
					require.NoError(t, json.Unmarshal(data, &last))
					times = append(times, last[0]["Execution Time"].(float64)+last[0]["Planning Time"].(float64))
				}
				slices.Sort(times)
				plan := last[0]["Plan"].(map[string]any)
				write("| %s | `%s` | %.3f | %v / %v | %v / %v | %v |\n", label, exec, times[1], plan["Shared Hit Blocks"], plan["Shared Read Blocks"], plan["Plan Rows"], plan["Actual Rows"], plan["Node Type"])
				// Retain the slowest plan for bottleneck inspection and per-node
				// estimates without duplicating every plan and large filter expression.
				compact, err := json.Marshal(compactAdminPlan(last[0]["Plan"].(map[string]any)))
				require.NoError(t, err)
				if times[1] > slowestTime {
					slowestTime = times[1]
					slowestPlan = label + "\n" + string(compact)
				}
				return times[1], true
			}
			// These reads are outside EXPLAIN timing. Expected memberships come
			// from the profile definition, never from another database query.
			assertPreviews := func(exec string, ids []string) {
				type preview struct {
					UserID, OrgID, Name, Slug string
					Count                     int64
				}
				expected := []preview{}
				orderedUsers := slices.Clone(ids)
				slices.Sort(orderedUsers)
				for _, id := range orderedUsers {
					require.True(t, strings.HasPrefix(id, "perf_user_"))
					user, err := strconv.Atoi(strings.TrimPrefix(id, "perf_user_"))
					require.NoError(t, err)
					require.GreaterOrEqual(t, user, 1)
					require.LessOrEqual(t, user, size)
					orgs := adminPerfMemberships(user, size/5)
					for _, org := range orgs[:min(3, len(orgs))] {
						expected = append(expected, preview{id, fmt.Sprintf("perf_org_%d", org), fmt.Sprintf("Fictional Studio %06d", org), fmt.Sprintf("fictional-studio-%06d", org), int64(len(orgs))})
					}
				}
				rows, err := conn.Query(ctx, exec)
				require.NoError(t, err)
				defer rows.Close()
				actual := []preview{}
				for rows.Next() {
					var row preview
					var disabledAt *time.Time
					require.NoError(t, rows.Scan(&row.UserID, &row.OrgID, &row.Name, &row.Slug, &disabledAt, &row.Count))
					require.Nil(t, disabledAt)
					actual = append(actual, row)
				}
				require.NoError(t, rows.Err())
				require.Equal(t, expected, actual, "exact preview ownership, order, identities and full per-user counts for %s", exec)
			}
			write("| Workload | Concrete EXECUTE parameters | Median planning + execution ms | Root buffers hit/read (run 3) | Root estimated/actual rows (run 3) | Root node |\n|---|---|---:|---:|---:|---|\n")
			for _, c := range cases {
				for _, offset := range []int{0, size / 2} {
					params := strings.ReplaceAll(c.params, "%%", "%")
					pageExec := fmt.Sprintf("EXECUTE adminListUsers(%s,%d,50)", params, offset)
					label := fmt.Sprintf("%s offset %d", c.name, offset)
					page, pok := measure(label+" page", pageExec)
					count, cok := measure(label+" count", "EXECUTE adminCountUsers("+params+")")
					if !pok || !cok {
						write("| %s TOTAL | incomplete: timeout; no request median | — | — | — | — |\n", label)
						continue
					}
					// Retrieve the actual page IDs, not invented preview IDs.
					rows, err := conn.Query(ctx, pageExec)
					require.NoError(t, err)
					ids := []string{}
					for rows.Next() {
						values, err := rows.Values()
						require.NoError(t, err)
						ids = append(ids, values[0].(string))
					}
					require.NoError(t, rows.Err())
					rows.Close()
					var total int64
					require.NoError(t, conn.QueryRow(ctx, "EXECUTE adminCountUsers("+params+")").Scan(&total))
					require.LessOrEqual(t, len(ids), 50)
					expected := map[string]int64{"empty": int64(size), "selective email / fanout preview": 1, "selective org": 11, "common bare": int64(size), "no match": 0, "one character": int64(size), "two characters": int64(size * 3 / 4), "mixed": 11, "repeated org": int64(size * 3 / 4)}[c.name]
					require.Equal(t, expected, total)
					require.Len(t, ids, int(min(int64(50), max(int64(0), total-int64(offset)))))
					preview := 0.0
					vok := true
					if len(ids) > 0 {
						previewExec := "EXECUTE adminListUsersOrganizationPreviews(" + adminPerfIDArray(ids) + ")"
						preview, vok = measure(label+" previews", previewExec)
						if vok {
							assertPreviews(previewExec, ids)
						}
					}
					if !vok {
						write("| %s TOTAL | incomplete: preview statement timeout | — | — | — | — |\n", label)
					}
					if vok {
						write("| %s TOTAL (sum of medians, not wall latency) | total=%d, returned=%d | %.3f | — | — | — |\n", label, total, len(ids), page+count+preview)
					}
				}
			}
			for _, offset := range []int{0, 150} {
				pageExec := fmt.Sprintf("EXECUTE adminListUserOrganizations('perf_user_1',%d,50)", offset)
				countExec := "EXECUTE adminCountUserOrganizations('perf_user_1')"
				_, pok := measure("200-org overflow page", pageExec)
				_, cok := measure("200-org overflow count", countExec)
				if !pok || !cok {
					write("| overflow offset %d assertions | INCOMPLETE: statement timeout | — | — | — | — |\n", offset)
					continue
				}
				var total int64
				require.NoError(t, conn.QueryRow(ctx, countExec).Scan(&total))
				require.EqualValues(t, 200, total)
				rows, err := conn.Query(ctx, pageExec)
				require.NoError(t, err)
				ids := []string{}
				for rows.Next() {
					var id, name, slug string
					var disabledAt *time.Time
					require.NoError(t, rows.Scan(&id, &name, &slug, &disabledAt))
					expectedOrg := offset + len(ids) + 1
					require.Equal(t, fmt.Sprintf("Fictional Studio %06d", expectedOrg), name)
					require.Equal(t, fmt.Sprintf("fictional-studio-%06d", expectedOrg), slug)
					require.Nil(t, disabledAt)
					ids = append(ids, id)
				}
				require.NoError(t, rows.Err())
				rows.Close()
				expectedIDs := []string{}
				for org := offset + 1; org <= offset+50; org++ {
					expectedIDs = append(expectedIDs, fmt.Sprintf("perf_org_%d", org))
				}
				require.Equal(t, expectedIDs, ids, "exact 200-org membership pagination")
				write("| overflow offset %d assertions | exact ordered org IDs %d–%d; count=200 verified | — | — | — | — |\n", offset, offset+1, offset+50)
			}
			if size == 100000 {
				write("\n### Isolated index / candidate-ID experiment (not shipped)\n\n")
				write("| Workload | Concrete EXECUTE parameters | Median planning + execution ms | Root buffers hit/read (run 3) | Root estimated/actual rows (run 3) | Root node |\n|---|---|---:|---:|---:|---|\n")
				_, err = conn.Exec(ctx, "PREPARE perfWrite AS UPDATE users SET display_name=display_name||' x' WHERE id IN (SELECT 'perf_user_'||i FROM generate_series(1,1000) i)")
				require.NoError(t, err)
				_, err = conn.Exec(ctx, "BEGIN")
				require.NoError(t, err)
				measure("write without GIN (1000 names, three updates rolled back)", "EXECUTE perfWrite")
				_, err = conn.Exec(ctx, "ROLLBACK")
				require.NoError(t, err)
				const indexes = `CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX perf_user_name_trgm ON users USING gin(display_name gin_trgm_ops);
CREATE INDEX perf_user_email_trgm ON users USING gin(email gin_trgm_ops);
CREATE INDEX perf_org_name_trgm ON organization_metadata USING gin(name gin_trgm_ops);
CREATE INDEX perf_org_slug_trgm ON organization_metadata USING gin(slug gin_trgm_ops);`
				_, err = conn.Exec(ctx, "SET statement_timeout='5s'")
				require.NoError(t, err)
				started := time.Now()
				_, err = conn.Exec(ctx, indexes)
				require.NoError(t, err)
				write("\nExperimental DDL, only in disposable clone (build wall time %s):\n\n```sql\n%s\n```\n\n", time.Since(started).Round(time.Millisecond), indexes)
				_, err = conn.Exec(ctx, "SET statement_timeout='1500ms'")
				require.NoError(t, err)
				const candidates = `WITH candidates AS (
SELECT id FROM users WHERE display_name ILIKE $1 OR email ILIKE $1
UNION
SELECT m.user_id FROM organization_metadata o JOIN organization_user_relationships m ON m.organization_id=o.id WHERE m.deleted IS FALSE AND (o.name ILIKE $1 OR o.slug ILIKE $1)
) SELECT u.id FROM users u JOIN candidates c ON c.id=u.id WHERE u.deleted_at IS NULL AND u.workos_deleted_at IS NULL`
				candidatePage := candidates + " ORDER BY lower(u.email),u.id OFFSET $2 LIMIT $3"
				candidateCount := "SELECT count(*) FROM (" + candidates + ") matched"
				write("Candidate shape is ONLY for one bare term, not a replacement for the full grammar.\n\n```sql\nPREPARE candidatePage AS %s;\nPREPARE candidateCount AS %s;\nPREPARE perfWrite AS UPDATE users SET display_name=display_name||' x' WHERE id IN (SELECT 'perf_user_'||i FROM generate_series(1,1000) i);\n```\n\n", candidatePage, candidateCount)
				_, err = conn.Exec(ctx, "PREPARE candidatePage AS "+candidatePage)
				require.NoError(t, err)
				_, err = conn.Exec(ctx, "PREPARE candidateCount AS "+candidateCount)
				require.NoError(t, err)
				write("| Workload | Concrete EXECUTE parameters | Median planning + execution ms | Root buffers hit/read (run 3) | Root estimated/actual rows (run 3) | Root node |\n|---|---|---:|---:|---:|---|\n")
				for _, term := range []string{"fictional", "absentneedle", "a", "st"} {
					beforeTimeouts := len(incomplete)
					params := "'{}','{}','{}','{%" + term + "%}'"
					for _, offset := range []int{0, 50000} {
						measure("GIN existing page "+term, fmt.Sprintf("EXECUTE adminListUsers(%s,%d,50)", params, offset))
						measure("GIN candidate page "+term, fmt.Sprintf("EXECUTE candidatePage('%%%s%%',%d,50)", term, offset))
					}
					measure("GIN existing count "+term, "EXECUTE adminCountUsers("+params+")")
					measure("GIN candidate count "+term, "EXECUTE candidateCount('%"+term+"%')")
					if len(incomplete) > beforeTimeouts {
						write("| experiment %s parity | INCOMPLETE: statement timeout; parity not checked | — | — | — | — |\n", term)
						continue
					}
					// Compare every ordered ID and exact count, not just timing.
					readIDs := func(sql string) []string {
						rows, err := conn.Query(ctx, sql)
						require.NoError(t, err)
						defer rows.Close()
						ids := []string{}
						for rows.Next() {
							v, err := rows.Values()
							require.NoError(t, err)
							ids = append(ids, v[0].(string))
						}
						require.NoError(t, rows.Err())
						return ids
					}
					actual := readIDs("EXECUTE adminListUsers(" + params + ",0,100001)")
					proposed := readIDs("EXECUTE candidatePage('%" + term + "%',0,100001)")
					require.Equal(t, actual, proposed)
					var count int64
					require.NoError(t, conn.QueryRow(ctx, "EXECUTE candidateCount('%"+term+"%')").Scan(&count))
					require.EqualValues(t, len(actual), count)
					write("| parity %s | all ordered IDs + count equal (%d) | — | — | — | — |\n", term, count)
				}
				_, err = conn.Exec(ctx, "BEGIN")
				require.NoError(t, err)
				measure("write with GIN (1000 names, three updates rolled back)", "EXECUTE perfWrite")
				_, err = conn.Exec(ctx, "ROLLBACK")
				require.NoError(t, err)
			}
			if len(incomplete) > 0 {
				write("\nINCOMPLETE profile: statement timeouts in %s. No full performance or assertion acceptance.\n", strings.Join(incomplete, ", "))
			} else {
				write("\nAll measurements completed; untimed exact preview identities/order/counts and both 200-org overflow pages/count passed.\n")
			}
			write("\nSlowest completed query, actual run-3 plan (compact node statistics):\n\n```text\n%s\n```\n\n", slowestPlan)
		})
	}
}

func adminPerfIDArray(ids []string) string {
	// IDs come only from the synthetic profile (letters, digits, underscore).
	return "'{" + strings.Join(ids, ",") + "}'::text[]"
}

// Keep useful plan statistics without repeating large filter/ID expressions.
func compactAdminPlan(plan map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"Node Type", "Relation Name", "Index Name", "Plan Rows", "Actual Rows", "Actual Loops", "Actual Total Time", "Shared Hit Blocks", "Shared Read Blocks", "Rows Removed by Filter"} {
		if v, ok := plan[key]; ok {
			out[key] = v
		}
	}
	if children, ok := plan["Plans"].([]any); ok {
		nested := []any{}
		for _, child := range children {
			nested = append(nested, compactAdminPlan(child.(map[string]any)))
		}
		out["Plans"] = nested
	}
	return out
}

func adminPerfStatementTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "57014" && pgErr.Message == "canceling statement due to statement timeout"
}

func TestAdminPerformanceStatementTimeout(t *testing.T) {
	timeout := &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
	for _, tc := range []struct {
		name    string
		err     error
		timeout bool
	}{
		{"nil", nil, false},
		{"statement timeout", timeout, true},
		{"wrapped timeout", fmt.Errorf("explain: %w", timeout), true},
		{"undefined prepared statement", &pgconn.PgError{Code: "26000", Message: "prepared statement does not exist"}, false},
		{"wrong parameter type", &pgconn.PgError{Code: "42804", Message: "datatype mismatch"}, false},
		{"explicit cancellation", &pgconn.PgError{Code: "57014", Message: "canceling statement due to user request"}, false},
		{"connection deadline", context.DeadlineExceeded, false},
		{"decoding error", errors.New("cannot decode JSON"), false},
		{"message alone", errors.New(timeout.Message), false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.timeout, adminPerfStatementTimeout(tc.err)) })
	}
}

// Names/slugs are zero-padded by the profile, so numeric org order is the
// expected lower(name), slug, id ordering, including memberships wrapping N.
func adminPerfMemberships(user, orgCount int) []int {
	orgs := []int{}
	if user == 1 {
		for org := 1; org <= 200; org++ {
			orgs = append(orgs, org)
		}
	} else {
		for j := 1; j <= user%4; j++ {
			orgs = append(orgs, (user+j-2)%orgCount+1)
		}
	}
	slices.Sort(orgs)
	return orgs
}

func TestAdminPerformanceMembershipExpectations(t *testing.T) {
	for _, tc := range []struct {
		user int
		want []int
	}{
		{2, []int{2, 3}}, {3, []int{3, 4, 5}}, {4, []int{}}, {5, []int{5}}, {1999, []int{1, 1999, 2000}},
	} {
		require.Equal(t, tc.want, adminPerfMemberships(tc.user, 2000))
	}
	want := []int{}
	for org := 1; org <= 200; org++ {
		want = append(want, org)
	}
	require.Equal(t, want, adminPerfMemberships(1, 2000))
	require.Equal(t, want, adminPerfMemberships(1, 20000))
}
