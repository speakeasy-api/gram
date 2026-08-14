package rule_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sqlclint/catalog"
	"github.com/speakeasy-api/gram/sqlclint/query"
	"github.com/speakeasy-api/gram/sqlclint/rule"
	"github.com/speakeasy-api/gram/sqlclint/schema"
)

// fixtureEngine parses the fixture schema and the catalog once. Both are
// immutable and the WASM-backed parser is the slowest part of a run, so sharing
// them keeps the table of cases cheap to extend.
var fixtureEngine = sync.OnceValues(func() (*rule.Engine, error) {
	tables, err := schema.NewFileSource("testdata/schema.sql").Tables(context.Background())
	if err != nil {
		return nil, err
	}

	cat, err := catalog.Load()
	if err != nil {
		return nil, err
	}

	return rule.NewEngine(schema.NewClassifier(tables), cat), nil
})

// check runs one query through the engine built on testdata/schema.sql.
func check(t *testing.T, sql string) rule.Result {
	t.Helper()

	engine, err := fixtureEngine()
	require.NoError(t, err)

	queries := query.Split("testdata/queries.sql", []byte(sql))
	require.Len(t, queries, 1, "fixture must contain exactly one query")

	return engine.Check(queries[0])
}

// requireClean asserts the query passes without needing an annotation.
func requireClean(t *testing.T, sql string) {
	t.Helper()
	res := check(t, sql)
	require.Emptyf(t, res.Diagnostics(), "expected no diagnostics, got %v", res.Diagnostics())
	require.True(t, res.Scoped)
}

// requireDiagnostic asserts exactly one diagnostic, with the given rule id.
func requireDiagnostic(t *testing.T, sql, ruleID string) rule.Diagnostic {
	t.Helper()
	res := check(t, sql)
	require.Lenf(t, res.Diagnostics(), 1, "expected one diagnostic, got %v", res.Diagnostics())
	require.Equal(t, ruleID, res.Diagnostics()[0].RuleID)
	return res.Diagnostics()[0]
}

func q(body string) string { return "-- name: Fixture :many\n" + body + "\n" }

// --- the required-column table, one test per shape ------------------------

func TestProjectNotNullRequiresProject(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM toolsets WHERE id = @id AND project_id = @project_id;`))
}

func TestProjectNotNullRejectsOrganizationOnly(t *testing.T) {
	t.Parallel()
	d := requireDiagnostic(t, q(
		`SELECT t.* FROM toolsets t JOIN projects p ON p.id = t.project_id
		 WHERE p.organization_id = @organization_id;`), catalog.WrongTenantColumn)
	require.Contains(t, d.Message, "organization_id")
	require.Contains(t, d.Message, "project_id")
}

// Both columns NOT NULL: project_id alone is the requirement, deliberately not
// both, so this must pass.
func TestBothNotNullIsSatisfiedByProjectAlone(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM deployments WHERE id = @id AND project_id = @project_id;`))
}

func TestOrganizationOnlyRequiresOrganization(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM projects WHERE organization_id = @organization_id;`))
}

// organization_id NOT NULL with a nullable project_id: binding only project_id
// silently drops rows whose project is NULL, so it must not satisfy the rule.
func TestNullableProjectDoesNotSatisfyGuaranteedOrganization(t *testing.T) {
	t.Parallel()
	requireDiagnostic(t, q(
		`SELECT * FROM api_keys WHERE project_id = @project_id;`), catalog.WrongTenantColumn)
}

func TestGuaranteedOrganizationIsSatisfiedByOrganization(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM api_keys WHERE organization_id = @organization_id;`))
}

func TestBothNullableAcceptsEitherColumn(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM external_credentials WHERE organization_id = @organization_id;`))
	requireClean(t, q(`SELECT * FROM external_credentials WHERE project_id = @project_id;`))
}

func TestNullableProjectWithNoOrganizationColumnRequiresProject(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM chat_messages WHERE project_id = @project_id;`))
	requireDiagnostic(t, q(`SELECT * FROM chat_messages WHERE id = @id;`), catalog.MissingTenantScope)
}

func TestChildTableInheritsItsParentRequirement(t *testing.T) {
	t.Parallel()
	requireDiagnostic(t, q(
		`SELECT * FROM toolset_versions WHERE toolset_id = @toolset_id;`), catalog.MissingTenantScope)
	requireClean(t, q(
		`SELECT v.* FROM toolset_versions v
		 WHERE v.toolset_id = @toolset_id
		   AND EXISTS (SELECT 1 FROM toolsets t WHERE t.id = v.toolset_id AND t.project_id = @project_id);`))
}

func TestGlobalTablesNeedNoBound(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM global_roles WHERE slug = @slug;`))
	requireClean(t, q(`SELECT * FROM global_roles g JOIN mcp_registries r ON r.id = g.id;`))
}

// A foreign key cycle among untenanted tables must terminate and classify as
// global rather than recursing.
func TestForeignKeyCycleTerminates(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM cycle_a a JOIN cycle_b b ON b.a_id = a.id;`))
}

// --- where a bound may appear ---------------------------------------------

func TestBoundIsAcceptedInAJoinCondition(t *testing.T) {
	t.Parallel()
	requireClean(t, q(
		`SELECT t.* FROM toolsets t
		 JOIN projects p ON p.id = t.project_id AND t.project_id = @project_id;`))
}

func TestBoundIsAcceptedInAnUpdateFrom(t *testing.T) {
	t.Parallel()
	requireClean(t, q(
		`UPDATE toolset_versions v SET version = version + 1
		 FROM toolsets t WHERE t.id = v.toolset_id AND t.project_id = @project_id;`))
}

func TestBoundIsAcceptedInsideACTE(t *testing.T) {
	t.Parallel()
	requireClean(t, q(
		`WITH scoped AS (SELECT id FROM toolsets WHERE project_id = @project_id)
		 SELECT * FROM scoped;`))
}

// A CTE name is not a table and must not be resolved as one.
func TestCTENamesAreNotTreatedAsTables(t *testing.T) {
	t.Parallel()
	requireClean(t, q(
		`WITH global_roles AS (SELECT @project_id::uuid AS project_id)
		 SELECT * FROM global_roles;`))
}

func TestBoundIsAcceptedWithAnAnyList(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM toolsets WHERE project_id = ANY(@project_ids::uuid[]);`))
}

func TestBoundIsAcceptedWithSqlcArgAndPositionalParams(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM toolsets WHERE project_id = sqlc.arg('project_id');`))
	requireClean(t, q(`SELECT * FROM toolsets WHERE project_id = $1;`))
}

func TestBoundIsAcceptedThroughACast(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`SELECT * FROM toolsets WHERE project_id = @project_id::uuid;`))
}

// A comparison against another column is a join key, not a tenant bound.
func TestColumnToColumnComparisonIsNotABound(t *testing.T) {
	t.Parallel()
	requireDiagnostic(t, q(
		`SELECT t.* FROM toolsets t JOIN deployments d ON d.project_id = t.project_id;`),
		catalog.MissingTenantScope)
}

// --- mutations -------------------------------------------------------------

func TestDeleteRequiresABound(t *testing.T) {
	t.Parallel()
	requireDiagnostic(t, q(`DELETE FROM toolsets WHERE id = @id;`), catalog.MissingTenantScope)
	requireClean(t, q(`DELETE FROM toolsets WHERE id = @id AND project_id = @project_id;`))
}

func TestUpdateTargetTableIsResolved(t *testing.T) {
	t.Parallel()
	requireDiagnostic(t, q(`UPDATE toolsets SET name = @name WHERE id = @id;`), catalog.MissingTenantScope)
}

func TestInsertIsScopedByItsValuesRow(t *testing.T) {
	t.Parallel()
	requireClean(t, q(`INSERT INTO toolsets (id, project_id, name) VALUES (@id, @project_id, @name);`))
	requireDiagnostic(t, q(
		`INSERT INTO toolsets (id, project_id, name)
		 SELECT @id, t.project_id, @name FROM toolsets t WHERE t.id = @source_id;`),
		catalog.MissingTenantScope)
}

// INSERT ... SELECT supplies columns positionally from the target list, so a
// parameter there is as much a bound as one in a VALUES row.
func TestInsertSelectIsScopedByItsTargetList(t *testing.T) {
	t.Parallel()
	requireClean(t, q(
		`INSERT INTO toolsets (project_id, id, name)
		 SELECT @project_id, @id, @name;`))
}

// --- nullable parameters ---------------------------------------------------

func TestNullableBoundIsRejected(t *testing.T) {
	t.Parallel()
	d := requireDiagnostic(t, q(
		`SELECT * FROM toolsets WHERE project_id = sqlc.narg('project_id');`),
		catalog.NullableTenantParam)
	require.Contains(t, d.Message, "sqlc.narg")
}

// A nullable filter is fine when a non-nullable bound already applies.
func TestNullableFilterIsFineBesideAStrictBound(t *testing.T) {
	t.Parallel()
	requireClean(t, q(
		`SELECT * FROM api_keys
		 WHERE organization_id = @organization_id
		   AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'));`))
}

// --- annotations -----------------------------------------------------------

func TestValidAnnotationExemptsTheQuery(t *testing.T) {
	t.Parallel()
	res := check(t, "-- name: Fixture :many\n-- sqlclint:ignore admin -- staff console, behind admin auth\nSELECT * FROM toolsets WHERE id = @id;\n")
	require.Empty(t, res.Diagnostics())
	require.True(t, res.Exempted)
	require.False(t, res.Scoped)
}

func TestUnknownCategoryIsRejectedAndDoesNotExempt(t *testing.T) {
	t.Parallel()
	res := check(t, "-- name: Fixture :many\n-- sqlclint:ignore not-a-category -- because\nSELECT * FROM toolsets WHERE id = @id;\n")
	require.False(t, res.Exempted)

	ids := make([]string, 0, len(res.Diagnostics()))
	for _, d := range res.Diagnostics() {
		ids = append(ids, d.RuleID)
	}
	require.ElementsMatch(t, []string{catalog.UnknownExemptionCategory, catalog.MissingTenantScope}, ids)
	require.Contains(t, res.Diagnostics()[0].Message, "global-table")
}

func TestAnnotationWithoutAReasonIsRejected(t *testing.T) {
	t.Parallel()
	res := check(t, "-- name: Fixture :many\n-- sqlclint:ignore admin\nSELECT * FROM toolsets WHERE id = @id;\n")
	require.False(t, res.Exempted)

	ids := make([]string, 0, len(res.Diagnostics()))
	for _, d := range res.Diagnostics() {
		ids = append(ids, d.RuleID)
	}
	require.ElementsMatch(t, []string{catalog.MissingExemptionReason, catalog.MissingTenantScope}, ids)
}

func TestAnnotationOnAScopedQueryIsRedundant(t *testing.T) {
	t.Parallel()
	res := check(t, "-- name: Fixture :many\n-- sqlclint:ignore admin -- staff console\nSELECT * FROM toolsets WHERE project_id = @project_id;\n")
	require.Len(t, res.Diagnostics(), 1)
	require.Equal(t, catalog.RedundantExemption, res.Diagnostics()[0].RuleID)
	require.False(t, res.Exempted)
}

func TestMultiLineAnnotationReasonIsJoined(t *testing.T) {
	t.Parallel()
	res := check(t, "-- name: Fixture :many\n-- sqlclint:ignore background-sweep -- outbox GC activity, runs on a\n-- timer with no request context\nSELECT * FROM toolsets WHERE id = @id;\n")
	require.Empty(t, res.Diagnostics())
	require.True(t, res.Exempted)
}

// --- structural failures ---------------------------------------------------

func TestUnparseableQueryFailsClosed(t *testing.T) {
	t.Parallel()
	requireDiagnostic(t, q(`SELECT * FROM toolsets WHERE id = @id AND;`), catalog.UnparseableQuery)
}

func TestUnknownTableFailsClosed(t *testing.T) {
	t.Parallel()
	d := requireDiagnostic(t, q(`SELECT * FROM toolsts WHERE project_id = @project_id;`),
		catalog.UnknownTableReference)
	require.Contains(t, d.Message, "toolsts")
}

// "FOR UPDATE OF t" names an alias, which Postgres represents as a RangeVar.
// Resolving it as a table would report every locked query as unknown.
func TestLockingClauseAliasIsNotATable(t *testing.T) {
	t.Parallel()
	requireClean(t, q(
		`SELECT * FROM toolsets t WHERE t.project_id = @project_id FOR UPDATE OF t;`))
}

func TestDiagnosticStringNamesTheRuleAndLocation(t *testing.T) {
	t.Parallel()
	d := requireDiagnostic(t, q(`SELECT * FROM toolsets WHERE id = @id;`), catalog.MissingTenantScope)
	require.Equal(t, "testdata/queries.sql", d.File)
	require.Equal(t, 2, d.Line)
	require.Contains(t, d.String(), fmt.Sprintf("[%s]", catalog.MissingTenantScope))
	require.Contains(t, d.String(), `query "Fixture"`)
}
