package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/stretchr/testify/require"
)

// Capture SQLc's actual statement and argument order, without copying its SQL.
type organizationPlanCapture struct {
	repo.DBTX
	sql  string
	args []any
}

func (c *organizationPlanCapture) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.sql, c.args = sql, args
	rows, err := c.DBTX.Query(ctx, sql, args...) //nolint:glint // notestingrawsql: wrapper forwards the SQLc-generated organization query while capturing its SQL and arguments for EXPLAIN
	if err != nil {
		return nil, fmt.Errorf("execute captured organization query: %w", err)
	}
	return rows, nil
}
func (c *organizationPlanCapture) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.sql, c.args = sql, args
	return c.DBTX.QueryRow(ctx, sql, args...) //nolint:glint // notestingrawsql: wrapper forwards the SQLc-generated organization query while capturing its SQL and arguments for EXPLAIN
}

//nolint:paralleltest,tparallel // Subtests share a connection, session planner mode, and mutable SQL capture; they must run sequentially.
func TestOrganizationQueryPlans(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := infra.CloneTestDatabase(t, "adminqueryplans")
	require.NoError(t, err)
	for i := range 24 {
		id := fmt.Sprintf("org_plan_%02d", i)
		seedOrg(t, ctx, pool, orgFixture{id: id, name: id, slug: id})
		// Deliberately reverse member counts relative to ID/name order.
		for j := 0; j < 24-i; j++ {
			seedMembership(t, ctx, pool, id, fmt.Sprintf("user_%02d", j))
		}
	}
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	//nolint:glint // notestingrawsql: Planner regression requires fresh PostgreSQL statistics, not a fixture query.
	_, err = conn.Exec(ctx, "ANALYZE organization_metadata; ANALYZE organization_user_relationships")
	require.NoError(t, err)
	var version string
	//nolint:glint // notestingrawsql: Report the PostgreSQL version that produced these plans.
	require.NoError(t, conn.QueryRow(ctx, "SHOW server_version").Scan(&version))
	t.Logf("PostgreSQL %s", version)
	capture := &organizationPlanCapture{DBTX: conn}
	queries := repo.New(capture)
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(mode, func(t *testing.T) {
			//nolint:glint // notestingrawsql: Session-level planner control must apply to the same connection as EXPLAIN.
			_, err := conn.Exec(ctx, "SET plan_cache_mode = "+mode)
			require.NoError(t, err)
			for _, tc := range []struct {
				name, sort string
				status     string
				desc       bool
				bound      bool
				offset     int64
				cursor     pgtype.Text
				scans      int
			}{
				{name: "active created", status: "active", sort: "created_at", desc: true, scans: 2},
				{name: "created default", sort: "created_at", desc: true, scans: 2},
				{name: "name", sort: "name", scans: 2},
				{name: "offset", sort: "name", offset: 5, scans: 2},
				{name: "legacy cursor", cursor: pgtype.Text{String: "org_plan_04", Valid: true}, scans: 2},
				{name: "members", sort: "member_count", scans: 24},
				{name: "members desc", sort: "member_count", desc: true, scans: 24},
				{name: "members bounded desc", sort: "member_count", desc: true, bound: true, scans: 24},
				{name: "members bounded", sort: "member_count", bound: true, scans: 24},
				{name: "name bounded", sort: "name", bound: true, scans: 24},
			} {
				t.Run(tc.name, func(t *testing.T) {
					status := tc.status
					if status == "" {
						status = "all"
					}
					p := repo.AdminListOrganizationsParams{DisabledStatus: status, PageLimit: 2, PageOffset: tc.offset, AfterID: tc.cursor, SortBy: tc.sort, SortDir: "asc"}
					if tc.desc {
						p.SortDir = "desc"
					}
					if tc.bound {
						p.MinMembers = pgtype.Int8{Int64: 2, Valid: true}
					}
					rows, err := queries.AdminListOrganizations(ctx, p)
					require.NoError(t, err)
					require.Len(t, rows, 2)
					if tc.sort == "member_count" {
						first := int64(1)
						if tc.bound {
							first = 2
						}
						second := first + 1
						if tc.desc {
							first = 24
							second = 23
						}
						require.Equal(t, first, rows[0].MemberCount)
						require.Equal(t, second, rows[1].MemberCount)
					}
					loops := organizationMembershipPlanLoops(t, conn.Conn(), capture.sql, capture.args)
					require.Equal(t, tc.scans, loops)
					t.Logf("list membership scan loops=%d", loops)
				})
			}
			for _, bounded := range []bool{false, true} {
				t.Run(fmt.Sprintf("count bounded=%t", bounded), func(t *testing.T) {
					p := repo.AdminCountOrganizationsParams{DisabledStatus: "all"}
					expected := int64(24)
					scans := 0
					if bounded {
						p.MinMembers = pgtype.Int8{Int64: 2, Valid: true}
						expected = 23
						scans = 24
					}
					count, err := queries.AdminCountOrganizations(ctx, p)
					require.NoError(t, err)
					require.Equal(t, expected, count)
					loops := organizationMembershipPlanLoops(t, conn.Conn(), capture.sql, capture.args)
					require.Equal(t, scans, loops)
					t.Logf("count membership scan loops=%d", loops)
				})
			}
		})
	}
}

// EXPLAIN EXECUTE exercises an explicitly prepared statement, so generic-plan
// coverage cannot accidentally explain a newly planned, parameterless query.
func organizationMembershipPlanLoops(t *testing.T, conn *pgx.Conn, sql string, args []any) int {
	t.Helper()
	ctx := t.Context()
	_, err := conn.Prepare(ctx, "organization_plan", sql)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Deallocate(ctx, "organization_plan")) }()
	literals := make([]string, len(args))
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	for i, arg := range args {
		switch v := arg.(type) {
		case string:
			literals[i] = quote(v)
		case int32:
			literals[i] = fmt.Sprint(v)
		case int64:
			literals[i] = fmt.Sprint(v)
		case []string:
			if v == nil {
				literals[i] = "NULL"
			} else {
				items := make([]string, len(v))
				for j, s := range v {
					items[j] = quote(s)
				}
				literals[i] = "ARRAY[" + strings.Join(items, ",") + "]::text[]"
			}
		case pgtype.Text:
			literals[i] = "NULL"
			if v.Valid {
				literals[i] = quote(v.String)
			}
		case pgtype.Int8:
			literals[i] = "NULL"
			if v.Valid {
				literals[i] = fmt.Sprint(v.Int64)
			}
		case pgtype.Bool:
			literals[i] = "NULL"
			if v.Valid {
				literals[i] = fmt.Sprint(v.Bool)
			}
		case pgtype.Timestamptz:
			require.False(t, v.Valid)
			literals[i] = "NULL"
		default:
			t.Fatalf("unsupported plan argument %T", arg)
		}
	}
	var raw []byte
	//nolint:glint // notestingrawsql: Explain the actual SQLc prepared statement; a generated fixture cannot express EXPLAIN EXECUTE.
	err = conn.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE organization_plan("+strings.Join(literals, ",")+")").Scan(&raw)
	require.NoError(t, err)
	var plan []struct {
		Plan organizationExplainNode `json:"Plan"`
	}
	require.NoError(t, json.Unmarshal(raw, &plan))
	require.Len(t, plan, 1)
	return plan[0].Plan.membershipLoops()
}

type organizationExplainNode struct {
	Relation string                    `json:"Relation Name"`
	Loops    int                       `json:"Actual Loops"`
	Plans    []organizationExplainNode `json:"Plans"`
}

func (n organizationExplainNode) membershipLoops() int {
	total := 0
	if n.Relation == "organization_user_relationships" {
		total = n.Loops
	}
	for _, child := range n.Plans {
		total += child.membershipLoops()
	}
	return total
}
