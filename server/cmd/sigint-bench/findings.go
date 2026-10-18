package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
	"golang.org/x/sync/errgroup"
)

type benchFixture struct {
	tenant      analytics.Tenant
	sensor      uuid.UUID
	signal      uuid.UUID
	from        time.Time
	now         time.Time
	perDay      int
	days        int
	sensors     int
	sessionSize int
}

type windowOracle struct {
	outcomes          int64
	answered          int64
	matches           int64
	evaluatedSessions int64
	matchedSessions   int64
	top               int64
	five              int64
}

// eventRange inverts the generator's second-rounded timestamps. Benchmark window
// boundaries are whole seconds; clamping also covers a window longer than history.
func eventRange(origin, from, to time.Time, perDay, total int) (int, int) {
	index := func(at time.Time) int {
		return max(0, min(total, int(((at.Unix()-origin.Unix())*int64(perDay)+86399)/86400)))
	}
	return index(from), index(to)
}

func (f benchFixture) oracle(from, to time.Time, custom bool) windowOracle {
	result := windowOracle{outcomes: 0, answered: 0, matches: 0, evaluatedSessions: 0, matchedSessions: 0, top: 0, five: 0}
	evaluated, matched := map[int]bool{}, map[int]int64{}
	first, last := eventRange(f.from, from, to, f.perDay, f.perDay*f.days)
	for event := first; event < last; event++ {
		if custom && event%7 > 1 {
			continue
		}
		result.outcomes++
		if event%100 == 0 {
			continue
		}
		result.answered++
		evaluated[event/f.sessionSize] = true
		if event%4 == 0 {
			result.matches++
			matched[event/f.sessionSize]++
		}
	}
	result.evaluatedSessions, result.matchedSessions = int64(len(evaluated)), int64(len(matched))
	counts := make([]int64, 0, len(matched))
	for _, count := range matched {
		counts = append(counts, count)
	}
	slices.Sort(counts)
	slices.Reverse(counts)
	if len(counts) > 0 {
		result.top = counts[0]
	}
	for _, count := range counts[:min(5, len(counts))] {
		result.five += count
	}
	return result
}

func (w windowOracle) check(row analytics.Row) error {
	var matches, sessions any = w.matches, w.matchedSessions
	if w.answered == 0 {
		matches, sessions = nil, nil
	}
	if row["observed_outcomes"] != w.outcomes || row["answered"] != w.answered || row["matched_messages"] != matches || row["evaluated_sessions"] != w.evaluatedSessions || row["matched_sessions"] != sessions {
		return fmt.Errorf("measurement disagrees with latest-attempt oracle: wanted %+v, got %v", w, row)
	}
	return nil
}

func extendedBench(ctx context.Context, conn clickhouse.Conn, f benchFixture, iterations, concurrency int) ([]result, error) {
	engine, err := analytics.NewEngine(nil, conn)
	if err != nil {
		return nil, fmt.Errorf("construct benchmark engine: %w", err)
	}
	request := signals.Request{Selection: signals.Selection{SensorID: f.sensor.String(), SignalID: f.signal.String(), MinimumLevel: nil, Threshold: .5}, Trigger: "stream", RunID: "", GroupBy: nil, Filters: nil, Groups: 5, Points: 2000}
	from := f.now.Add(-7 * 24 * time.Hour)
	window := analytics.Window{FromUnixNano: from.UnixNano(), ToUnixNano: f.now.UnixNano()}
	compileContext := signals.Context{OrganizationID: f.tenant.OrganizationID, ProjectID: f.tenant.ProjectID, From: from, To: f.now, Now: f.now, Step: "auto", RequestedAt: nil}
	plan, err := signals.Compile(compileContext, request)
	if err != nil {
		return nil, fmt.Errorf("compile session benchmark: %w", err)
	}
	peer := request.Selection
	if f.sensors > 1 {
		peer.SensorID = "00000000-0000-4000-8000-000000000002"
	}
	want := f.oracle(from, f.now, false)
	findings, err := measure(ctx, conn, "7d-session-findings", iterations, plan.Summary(), func(ctx context.Context) error {
		got, err := engine.SignalFindings(ctx, f.tenant, window, analytics.SignalFindingsRequest{Query: request, Peer: &peer, ComparePrevious: true, Evidence: false}, nil)
		if err != nil {
			return fmt.Errorf("session findings: %w", err)
		}
		if err := want.check(got.Measurement.Summary); err != nil {
			return err
		}
		if err := want.check(got.PeerSummary); err != nil {
			return err
		}
		if err := f.oracle(from.Add(-7*24*time.Hour), from, false).check(got.Comparison.Summary); err != nil {
			return err
		}
		if got.Concentration["top_session_matches"] != want.top || got.Concentration["top_five_session_matches"] != want.five {
			return fmt.Errorf("concentration disagrees with oracle")
		}
		if got.Cooccurrence[0]["joint_answered"] != want.answered || got.Cooccurrence[0]["joint_matches"] != want.matches || got.Cooccurrence[1]["joint_answered"] != want.evaluatedSessions || got.Cooccurrence[1]["joint_matches"] != want.matchedSessions {
			return fmt.Errorf("cooccurrence disagrees with oracle")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	from = f.now.Add(-6 * time.Hour)
	window.FromUnixNano = from.UnixNano()
	compileContext.From = from
	plan, err = signals.Compile(compileContext, request)
	if err != nil {
		return nil, fmt.Errorf("compile evidence benchmark: %w", err)
	}
	evidenceQuery := plan.Evidence(nil)
	evidence, err := measure(ctx, conn, "6h-evidence-candidates", iterations, evidenceQuery, func(ctx context.Context) error {
		rows, err := (&analytics.Plan{Dataset: signals.Dataset, Name: "evidence", SQL: evidenceQuery.SQL, Args: evidenceQuery.Args, Columns: nil}).Run(ctx, conn)
		if err != nil {
			return fmt.Errorf("read evidence candidates: %w", err)
		}
		seen := map[string]bool{}
		first, last := eventRange(f.from, from, f.now, f.perDay, f.perDay*f.days)
		for _, row := range rows {
			event, err := strconv.Atoi(fmt.Sprint(row["message_id"]))
			if err != nil {
				return fmt.Errorf("parse synthetic event: %w", err)
			}
			conversation := fmt.Sprint(row["conversation_id"])
			if event < first || event >= last || event%4 != 0 || event%100 == 0 || seen[conversation] {
				return fmt.Errorf("evidence disagrees with latest-attempt oracle")
			}
			seen[conversation] = true
		}
		if len(rows) > signals.EvidenceCandidates {
			return fmt.Errorf("evidence exceeded candidate budget")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	custom := request
	custom.Filters = []signals.Filter{{Dimension: signals.Dimension{Field: "", Attribute: "custom.cohort"}, Operator: "in", Values: []string{"cohort-0", "cohort-1"}}}
	customPlan, err := signals.Compile(compileContext, custom)
	if err != nil {
		return nil, fmt.Errorf("compile custom cohort: %w", err)
	}
	filtered, err := measure(ctx, conn, "6h-arbitrary-attribute", iterations, customPlan.Summary(), func(ctx context.Context) error {
		got, err := engine.SignalQuery(ctx, f.tenant, window, "minute", custom)
		if err != nil {
			return fmt.Errorf("custom cohort: %w", err)
		}
		return f.oracle(from, f.now, true).check(got.Summary)
	})
	if err != nil {
		return nil, err
	}
	manual := request
	manual.Trigger, manual.RunID = "manual", uuid.NewString()
	if err := conn.Exec(ctx, `INSERT INTO sigint_readings_v2 SELECT * REPLACE
 (generateUUIDv4() AS id, generateUUIDv4() AS evaluation_attempt_id, 'manual' AS trigger_kind, toUUID(?) AS run_id,
 toDateTime64(?,9,'UTC') AS evaluation_requested_at, toDateTime64(?,9,'UTC') AS evaluated_at)
 FROM (SELECT * FROM sigint_readings_v2 FINAL WHERE organization_id=? AND project_id=toUUID(?) AND trigger_kind='stream' AND run_id=toUUID(?) AND sensor_id=toUUID(?) AND event_occurred_at>=? AND event_occurred_at<?)
 SETTINGS max_execution_time=5, max_rows_to_read=25000000, max_memory_usage=1000000000, do_not_merge_across_partitions_select_final=1`, manual.RunID, f.now, f.now, f.tenant.OrganizationID, f.tenant.ProjectID, uuid.Nil.String(), f.sensor.String(), from, f.now); err != nil {
		return nil, fmt.Errorf("seed manual cohort: %w", err)
	}
	compileContext.RequestedAt = &f.now
	manualPlan, err := signals.Compile(compileContext, manual)
	if err != nil {
		return nil, fmt.Errorf("compile manual cohort: %w", err)
	}
	manualResult, err := measure(ctx, conn, "6h-manual-partition", iterations, manualPlan.Summary(), func(ctx context.Context) error {
		got, err := engine.SignalQuery(ctx, f.tenant, window, "minute", manual)
		if err != nil {
			return fmt.Errorf("manual cohort: %w", err)
		}
		return f.oracle(from, f.now, false).check(got.Summary)
	})
	if err != nil {
		return nil, err
	}
	concurrent, err := concurrentCharts(ctx, conn, engine, f, request, window, iterations, concurrency, false)
	if err != nil {
		return nil, err
	}
	fresh, err := concurrentCharts(ctx, conn, engine, f, request, window, iterations, concurrency, true)
	if err != nil {
		return nil, err
	}
	return []result{findings, evidence, filtered, manualResult, concurrent, fresh}, nil
}

func measure(ctx context.Context, conn clickhouse.Conn, name string, iterations int, query signals.Query, run func(context.Context) error) (result, error) {
	m := result{Name: name, ColdMS: 0, P50MS: 0, P95MS: 0, ReadRows: 0, ReadBytes: 0, MemoryBytes: 0, OracleMatched: false, Explain: nil, Refusal: "", SelectedMarks: 0, Estimate: nil}
	for _, reset := range []string{"SYSTEM DROP MARK CACHE", "SYSTEM DROP UNCOMPRESSED CACHE", "SYSTEM DROP QUERY CACHE"} {
		if err := conn.Exec(ctx, reset); err != nil {
			return m, fmt.Errorf("reset caches: %w", err)
		}
	}
	estimate, err := (&analytics.Plan{SQL: "EXPLAIN ESTIMATE " + diagnosticSQL(query.SQL), Args: query.Args, Dataset: signals.Dataset, Name: "estimate", Columns: nil}).Run(ctx, conn)
	if err != nil {
		return m, fmt.Errorf("estimate %s: %w", name, err)
	}
	m.Estimate = estimate
	explain, err := (&analytics.Plan{SQL: "EXPLAIN indexes = 1 " + diagnosticSQL(query.SQL) + ", use_query_condition_cache = 0, use_skip_indexes_on_data_read = 0", Args: query.Args, Dataset: signals.Dataset, Name: "explain", Columns: nil}).Run(ctx, conn)
	if err != nil {
		return m, fmt.Errorf("explain %s: %w", name, err)
	}
	for _, row := range explain {
		for _, value := range row {
			m.Explain = append(m.Explain, fmt.Sprint(value))
		}
	}
	times := make([]float64, 0, iterations)
	queryID := ""
	for i := 0; i <= iterations; i++ {
		queryID = fmt.Sprintf("sigint-extended-%s-%d", name, i)
		begin := time.Now()
		if err := run(clickhouse.Context(ctx, clickhouse.WithQueryID(queryID))); err != nil {
			return m, fmt.Errorf("benchmark %s: %w", name, err)
		}
		elapsed := float64(time.Since(begin).Microseconds()) / 1000
		if i == 0 {
			m.ColdMS = elapsed
		} else {
			times = append(times, elapsed)
		}
	}
	slices.Sort(times)
	m.P50MS, m.P95MS, m.OracleMatched = times[len(times)/2], times[(len(times)*95+99)/100-1], true
	if err := readDiagnostics(ctx, conn, queryID, &m); err != nil {
		return m, err
	}
	return m, nil
}

func readDiagnostics(ctx context.Context, conn clickhouse.Conn, queryID string, m *result) error {
	if err := conn.Exec(ctx, "SYSTEM FLUSH LOGS"); err != nil {
		return fmt.Errorf("flush diagnostics: %w", err)
	}
	if err := conn.QueryRow(ctx, `SELECT sum(read_rows), sum(read_bytes), max(memory_usage), sum(ProfileEvents['SelectedMarks']) FROM system.query_log WHERE query_id=? AND type='QueryFinish'`, queryID).Scan(&m.ReadRows, &m.ReadBytes, &m.MemoryBytes, &m.SelectedMarks); err != nil {
		return fmt.Errorf("read diagnostics: %w", err)
	}
	return nil
}

func concurrentCharts(ctx context.Context, conn clickhouse.Conn, engine *analytics.Engine, f benchFixture, request signals.Request, window analytics.Window, iterations, concurrency int, fresh bool) (result, error) {
	m := result{Name: "6h-concurrent-with-duplicate-ingestion", ColdMS: 0, P50MS: 0, P95MS: 0, ReadRows: 0, ReadBytes: 0, MemoryBytes: 0, OracleMatched: false, Explain: nil, Refusal: "", SelectedMarks: 0, Estimate: nil}
	if fresh {
		m.Name = "6h-concurrent-with-new-ingestion"
	}
	group, workCtx := errgroup.WithContext(ctx)
	group.SetLimit(concurrency + 1)
	var lock sync.Mutex
	times := make([]float64, 0, concurrency*iterations)
	want := f.oracle(time.Unix(0, window.FromUnixNano), f.now, false)
	for worker := range concurrency {
		group.Go(func() error {
			for i := range iterations {
				begin := time.Now()
				queryID := fmt.Sprintf("sigint-concurrent-%t-%d-%d", fresh, worker, i)
				got, err := engine.SignalQuery(clickhouse.Context(workCtx, clickhouse.WithQueryID(queryID)), f.tenant, window, "minute", request)
				if err != nil {
					return fmt.Errorf("concurrent chart: %w", err)
				}
				if err := want.check(got.Summary); err != nil {
					return err
				}
				elapsed := float64(time.Since(begin).Microseconds()) / 1000
				lock.Lock()
				times = append(times, elapsed)
				lock.Unlock()
			}
			return nil
		})
	}
	group.Go(func() error {
		// Bounded batches overlap readers. New observations arrive after the pinned
		// source-time cutoff; duplicate deliveries retain their original coordinates.
		const ingestionBatches = 16
		for range ingestionBatches {
			source := `SELECT * FROM sigint_readings_v2 FINAL WHERE organization_id=? AND project_id=toUUID(?) AND trigger_kind='stream' AND run_id=toUUID(?) AND sensor_id=toUUID(?) AND event_occurred_at>=? AND event_occurred_at<? LIMIT 1000`
			query := "INSERT INTO sigint_readings_v2 " + source
			args := []any{f.tenant.OrganizationID, f.tenant.ProjectID, uuid.Nil.String(), f.sensor.String(), time.Unix(0, window.FromUnixNano), f.now}
			if fresh {
				at := time.Now().UTC()
				query = `INSERT INTO sigint_readings_v2 SELECT * REPLACE (generateUUIDv4() AS id, generateUUIDv4() AS evaluation_attempt_id, toString(generateUUIDv4()) AS event_id,
 toDateTime64(?,9,'UTC') AS event_occurred_at, toDateTime64(?,9,'UTC') AS evaluation_requested_at, toDateTime64(?,9,'UTC') AS evaluated_at) FROM (` + source + ")"
				args = append([]any{at, at, at}, args...)
			}
			query += " SETTINGS max_execution_time=5, max_rows_to_read=25000000, max_memory_usage=1000000000, do_not_merge_across_partitions_select_final=1"
			if err := conn.Exec(workCtx, query, args...); err != nil {
				return fmt.Errorf("concurrent duplicate ingestion: %w", err)
			}
		}
		return nil
	})
	if err := group.Wait(); err != nil {
		return m, fmt.Errorf("concurrent benchmark: %w", err)
	}
	slices.Sort(times)
	m.P50MS, m.P95MS, m.OracleMatched = times[len(times)/2], times[(len(times)*95+99)/100-1], true
	if err := readDiagnostics(ctx, conn, fmt.Sprintf("sigint-concurrent-%t-%d-%d", fresh, concurrency-1, iterations-1), &m); err != nil {
		return m, err
	}
	return m, nil
}
