//go:build projectresourceindexdbbench

package projectresourceindex

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/projectresourceindex/repo"
)

type databaseQueryResult struct {
	kind    string
	count   int
	elapsed time.Duration
	err     error
}

// TestSyntheticDatabaseQueryEvidence writes temporary assistant rows to an
// existing project in a loopback-only local database. It is opt-in because
// ordinary tests must remain database-independent and read-only. The unique
// created_by_user_id marker makes cleanup exact and safe to retry.
//
//nolint:paralleltest // opt-in benchmark mutates one shared local database project.
func TestSyntheticDatabaseQueryEvidence(t *testing.T) {
	if os.Getenv("PROJECT_RESOURCE_INDEX_DB_BENCH") != "1" {
		return
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("GRAM_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if host := pool.Config().ConnConfig.Host; !isLoopbackHost(host) {
		t.Fatalf("refusing benchmark fixture writes to non-loopback database host %q", host)
	}

	var projectID uuid.UUID
	var organizationID string
	//nolint:notestingrawsql // opt-in local-only benchmark fixture discovery is not product data access.
	if err := pool.QueryRow(ctx, `
		SELECT p.id, p.organization_id FROM projects p WHERE p.deleted IS FALSE
		ORDER BY
			(SELECT count(*) FROM assistants a WHERE a.project_id = p.id AND a.deleted IS FALSE) +
			(SELECT count(*) FROM environments e WHERE e.project_id = p.id AND e.deleted IS FALSE) +
			(SELECT count(*) FROM meta_mcp_servers g WHERE g.project_id = p.id AND g.deleted IS FALSE) +
			(SELECT count(*) FROM mcp_servers m WHERE m.project_id = p.id AND m.deleted IS FALSE) +
			(SELECT count(*) FROM plugins x WHERE x.project_id = p.id AND x.deleted IS FALSE) +
			(SELECT count(*) FROM risk_custom_detection_rules r WHERE r.project_id = p.id AND r.deleted IS FALSE) +
			(SELECT count(*) FROM skills s WHERE s.project_id = p.id AND s.archived_at IS NULL) DESC
		LIMIT 1`).Scan(&projectID, &organizationID); err != nil {
		t.Fatal(err)
	}

	marker := "project-resource-index-bench-" + uuid.NewString()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		//nolint:notestingrawsql // benchmark fixtures require guaranteed exact cleanup.
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM assistants WHERE project_id = $1 AND created_by_user_id = $2`, projectID, marker); err != nil {
			t.Errorf("clean synthetic assistant fixtures: %v", err)
		}
	})

	queries := repo.New(pool)
	baseline, baselineWall := runSevenDatabaseQueries(t, ctx, queries, projectID, organizationID)
	currentTotal := totalDatabaseItems(baseline)
	fmt.Printf("BASELINE\titems=%d\tconcurrent_wall_ms=%.3f\tquery_count=7\n", currentTotal, durationMS(baselineWall))
	fmt.Println("target_items\tinserted_this_step\tinsert_setup_ms\tkind\titems\tquery_ms\tconcurrent_wall_ms")
	for _, target := range []int{1_000, 10_000, 50_000} {
		toInsert := target - currentTotal
		if toInsert < 0 {
			t.Fatalf("seeded project already has %d indexed items, above target %d", currentTotal, target)
		}
		insertStarted := time.Now()
		if toInsert > 0 {
			//nolint:notestingrawsql // opt-in benchmark bulk fixture generation is intentionally synthetic.
			_, err := pool.Exec(ctx, `
				INSERT INTO assistants (project_id, organization_id, created_by_user_id, name, model, instructions)
				SELECT $1, $2, $3, $4 || '-' || lpad(series::text, 6, '0'), 'benchmark-model', ''
				FROM generate_series($5::int, $6::int) AS series`, projectID, organizationID, marker, marker, currentTotal+1, target)
			if err != nil {
				t.Fatalf("insert %d synthetic assistants for target %d: %v", toInsert, target, err)
			}
		}
		insertElapsed := time.Since(insertStarted)
		results, wall := runSevenDatabaseQueries(t, ctx, queries, projectID, organizationID)
		actualTotal := totalDatabaseItems(results)
		if actualTotal != target {
			t.Fatalf("target %d returned %d indexed items", target, actualTotal)
		}
		for _, result := range results {
			fmt.Printf("%d\t%d\t%.3f\t%s\t%d\t%.3f\t%.3f\n", target, toInsert, durationMS(insertElapsed), result.kind, result.count, durationMS(result.elapsed), durationMS(wall))
		}
		currentTotal = actualTotal
	}
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func runSevenDatabaseQueries(t *testing.T, ctx context.Context, queries *repo.Queries, projectID uuid.UUID, organizationID string) ([]databaseQueryResult, time.Duration) {
	t.Helper()
	out := make(chan databaseQueryResult, 7)
	var start sync.WaitGroup
	start.Add(1)
	run := func(kind string, fn func() (int, error)) {
		go func() {
			start.Wait()
			began := time.Now()
			count, err := fn()
			out <- databaseQueryResult{kind, count, time.Since(began), err}
		}()
	}
	run("assistant", func() (int, error) {
		rows, e := queries.ListProjectResourceIndexAssistants(ctx, projectID)
		return len(rows), e
	})
	run("environment", func() (int, error) {
		rows, e := queries.ListProjectResourceIndexEnvironments(ctx, projectID)
		return len(rows), e
	})
	run("gateway", func() (int, error) {
		rows, e := queries.ListProjectResourceIndexGateways(ctx, repo.ListProjectResourceIndexGatewaysParams{OrganizationID: organizationID, ProjectID: projectID})
		return len(rows), e
	})
	run("mcp", func() (int, error) {
		rows, e := queries.ListProjectResourceIndexMCPServers(ctx, projectID)
		return len(rows), e
	})
	run("plugin", func() (int, error) {
		rows, e := queries.ListProjectResourceIndexPlugins(ctx, repo.ListProjectResourceIndexPluginsParams{OrganizationID: organizationID, ProjectID: projectID})
		return len(rows), e
	})
	run("rule", func() (int, error) {
		rows, e := queries.ListProjectResourceIndexCustomRules(ctx, repo.ListProjectResourceIndexCustomRulesParams{OrganizationID: organizationID, ProjectID: projectID})
		return len(rows), e
	})
	run("skill", func() (int, error) {
		rows, e := queries.ListProjectResourceIndexSkills(ctx, projectID)
		return len(rows), e
	})
	wallStarted := time.Now()
	start.Done()
	results := make([]databaseQueryResult, 0, 7)
	for range 7 {
		result := <-out
		if result.err != nil {
			t.Fatalf("%s query: %v", result.kind, result.err)
		}
		results = append(results, result)
	}
	wall := time.Since(wallStarted)
	sort.Slice(results, func(i, j int) bool { return results[i].kind < results[j].kind })
	return results, wall
}

func totalDatabaseItems(results []databaseQueryResult) int {
	total := 0
	for _, result := range results {
		total += result.count
	}
	return total
}

func durationMS(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }
