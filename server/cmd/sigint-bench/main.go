// sigint-bench measures the shared canonical compiler on synthetic, replaceable
// observations. It owns a disposable ClickHouse and never connects to deployment data.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	// image pins the production engine generation independently of the normal test image.
	image = "clickhouse/clickhouse-server:26.6.8.7@sha256:2c870d77ea76e955fb1c8dad536ad40a1236b6093db0c4966e22d21d4428982b"
	// insertBatch bounds parts and generator memory while retaining overlapping attempts.
	insertBatch = 100000
	// benchmarkTimeout bounds fixture construction and the entire local experiment.
	benchmarkTimeout = 30 * time.Minute
)

type result struct {
	// Name identifies the source-time window and displayed interval.
	Name string `json:"name"`
	// ColdMS is the first run after dropping in-process read caches.
	ColdMS float64 `json:"cold_ms"`
	// P50MS is the median of the subsequent repeated runs.
	P50MS float64 `json:"p50_ms"`
	// P95MS is the empirical upper percentile of the repeated runs.
	P95MS float64 `json:"p95_ms"`
	// ReadRows is the last query's measured physical scan size.
	ReadRows uint64 `json:"read_rows"`
	// ReadBytes is the last query's measured decompressed scan volume.
	ReadBytes uint64 `json:"read_bytes"`
	// MemoryBytes is the last query's peak memory.
	MemoryBytes uint64 `json:"memory_bytes"`
	// OracleMatched confirms exact latest-attempt counts against the generator.
	OracleMatched bool `json:"oracle_matched"`
	// Explain records actual index analysis under pinned diagnostic settings.
	Explain []string `json:"explain"`
	// Refusal records an enforced cost limit rather than hiding a failed case.
	Refusal string `json:"refusal,omitempty"`
}

type report struct {
	// Version is the engine-reported version, not merely the image tag.
	Version string `json:"version"`
	// LogicalReadings excludes duplicated attempts.
	LogicalReadings int `json:"logical_readings"`
	// ReadingsPerDay is per sensor.
	ReadingsPerDay int `json:"readings_per_sensor_day"`
	// Days is the source-time history length.
	Days int `json:"days"`
	// Sensors is the number of independently keyed sensors.
	Sensors int `json:"sensors"`
	// Iterations records the sample size behind the percentiles.
	Iterations int `json:"iterations"`
	// Parts records merge state during the measurement.
	Parts uint64 `json:"parts"`
	// EventTimeIndex records whether the experimental immutable-time index was used.
	EventTimeIndex bool `json:"event_time_index"`
	// Results includes cold and warm canonical read evidence.
	Results []result `json:"results"`
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(context.Background(), logger); err != nil {
		logger.ErrorContext(context.Background(), "signal benchmark", attr.SlogError(err))
		os.Exit(1)
	}
}

func run(parent context.Context, logger *slog.Logger) error {
	perDay := flag.Int("readings-per-day", 1000, "logical readings per sensor/day")
	days := flag.Int("days", 730, "days of history")
	sensors := flag.Int("sensors", 3, "sensor count")
	iterations := flag.Int("iterations", 5, "warm measurements per case")
	eventTimeIndex := flag.Bool("event-time-index", false, "test source-time minmax pruning")
	out := flag.String("out", "/tmp/opencode/sigint-benchmark.json", "report path")
	flag.Parse()
	if *perDay < 1 || *perDay > 100000 || *days < 1 || *days > 730 || *sensors < 1 || *sensors > 50 || *iterations < 1 || *iterations > 30 {
		return fmt.Errorf("invalid workload bounds")
	}
	ctx, cancel := context.WithTimeout(parent, benchmarkTimeout)
	defer cancel()
	container, err := clickhousecontainer.Run(ctx, image,
		clickhousecontainer.WithUsername("benchmark"), clickhousecontainer.WithPassword("benchmark"),
		clickhousecontainer.WithInitScripts("clickhouse/schema.sql"),
		testcontainers.WithConfigModifier(func(config *dockercontainer.Config) { config.Hostname = "sigint-benchmark" }),
		testcontainers.WithWaitStrategy(wait.ForExec([]string{"clickhouse-client", "--host", "sigint-benchmark", "--user", "benchmark", "--password", "benchmark", "--query", "SELECT 1"})),
		testenv.WithPublishedPortWait("9000/tcp"), testenv.WithLoopbackPorts())
	if err != nil {
		return fmt.Errorf("start isolated engine: %w", err)
	}
	defer o11y.LogDefer(ctx, logger, "stop benchmark container", func() error { return container.Terminate(context.WithoutCancel(ctx)) })
	addr, err := testenv.ContainerAddr(ctx, container, "9000/tcp")
	if err != nil {
		return fmt.Errorf("resolve benchmark listener: %w", err)
	}
	conn, err := clickhouse.Open(&clickhouse.Options{Addr: []string{addr}, Auth: clickhouse.Auth{Database: "default", Username: "benchmark", Password: "benchmark"}, Settings: clickhouse.Settings{"max_threads": 4, "use_query_cache": 0, "use_query_condition_cache": 0}})
	if err != nil {
		return fmt.Errorf("open benchmark connection: %w", err)
	}
	defer o11y.LogDefer(ctx, logger, "close benchmark connection", conn.Close)
	// Keep the baseline independent of which indexes the current schema ships.
	if err := conn.Exec(ctx, "ALTER TABLE sigint_readings_v2 DROP INDEX IF EXISTS event_occurred_at_minmax_idx"); err != nil {
		return fmt.Errorf("prepare unindexed benchmark baseline: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	from := now.Add(-time.Duration(*days) * 24 * time.Hour)
	project := uuid.NewSHA1(uuid.Nil, []byte("benchmark-project"))
	sensor := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	signal := uuid.NewSHA1(uuid.Nil, []byte("benchmark-signal"))
	const org = "synthetic-signal-benchmark"
	total := *perDay * *days * *sensors
	for start := 0; start < total; start += insertBatch {
		count := min(insertBatch, total-start)
		err := conn.Exec(ctx, `INSERT INTO sigint_readings_v2
 (id, evaluation_attempt_id, organization_id, project_id, trigger_kind, run_id, sensor_id, sensor_slug, event_kind, event_id, event_occurred_at, evaluation_requested_at, evaluated_at, inserted_at, status, definition_hash, result_type, probabilities, conversation_id, conversation_capture_kind, attributes)
 SELECT toUUID(concat('10000000-0000-4000-8000-', leftPad(toString(number + 1), 12, '0'))) AS id, id, ?, toUUID(?), 'stream', toUUID(?),
 toUUID(concat('00000000-0000-4000-8000-', leftPad(toString(number % ? + 1), 12, '0'))), 'synthetic', 'conversation.message', toString(intDiv(number, ?)),
 addSeconds(toDateTime64(?, 9, 'UTC'), intDiv(intDiv(number, ?) * 86400, ?)) AS event_time,
 if(intDiv(number, ?) % 20 = 0, toDateTime64(?, 9, 'UTC'), least(addHours(event_time, 1), toDateTime64(?, 9, 'UTC'))) AS requested,
 requested, toDateTime64(?, 9, 'UTC'), 'answered', 'synthetic-definition', 'multi_label',
 [(toUUID(?), 'target', if(intDiv(number, ?) % 4 = 0, 0.9, 0.1))],
 toUUID(concat('20000000-0000-4000-8000-', leftPad(toString(intDiv(intDiv(number, ?), 100) + 1), 12, '0'))), 'original',
 map('department', concat('team-', toString(intDiv(number, ?) % 5)), 'model_provider', if(number % 2 = 0, 'openrouter', 'bedrock'))
 FROM numbers(?, ?)`, org, project.String(), uuid.Nil.String(), *sensors, *sensors,
			from, *sensors, *perDay, *sensors, now, now, now, signal.String(), *sensors, *sensors, *sensors, start, count)
		if err != nil {
			return fmt.Errorf("generate readings: %w", err)
		}
	}
	// Duplicate deliveries and answer-to-failure corrections share the replacement key.
	if err := conn.Exec(ctx, "INSERT INTO sigint_readings_v2 SELECT * FROM sigint_readings_v2 WHERE toUInt64(event_id) % 100 = 0"); err != nil {
		return fmt.Errorf("insert duplicate attempts: %w", err)
	}
	if err := conn.Exec(ctx, "INSERT INTO sigint_readings_v2 SELECT * REPLACE (? AS evaluated_at, 'failed' AS status, '' AS result_type, [] AS probabilities) FROM sigint_readings_v2 FINAL WHERE toUInt64(event_id) % 100 = 0", now.Add(time.Second)); err != nil {
		return fmt.Errorf("insert corrected attempts: %w", err)
	}
	if *eventTimeIndex {
		if err := conn.Exec(ctx, "ALTER TABLE sigint_readings_v2 ADD INDEX bench_event_time event_occurred_at TYPE minmax GRANULARITY 1"); err != nil {
			return fmt.Errorf("add benchmark time index: %w", err)
		}
		if err := conn.Exec(ctx, "ALTER TABLE sigint_readings_v2 MATERIALIZE INDEX bench_event_time SETTINGS mutations_sync = 1"); err != nil {
			return fmt.Errorf("materialize benchmark time index: %w", err)
		}
	}
	r := report{Version: "", LogicalReadings: total, ReadingsPerDay: *perDay, Days: *days, Sensors: *sensors, Iterations: *iterations, Parts: 0, Results: nil, EventTimeIndex: *eventTimeIndex}
	if err := conn.QueryRow(ctx, "SELECT version()").Scan(&r.Version); err != nil {
		return fmt.Errorf("read engine version: %w", err)
	}
	if err := conn.QueryRow(ctx, "SELECT count() FROM system.parts WHERE active AND database = 'default' AND table = 'sigint_readings_v2'").Scan(&r.Parts); err != nil {
		return fmt.Errorf("read part count: %w", err)
	}
	for _, c := range []struct {
		name, step string
		span       time.Duration
	}{
		{name: "6h-minute", step: "minute", span: 6 * time.Hour}, {name: "7d-hour", step: "hour", span: 7 * 24 * time.Hour},
		{name: "90d-day", step: "day", span: 90 * 24 * time.Hour}, {name: "24mo-month", step: "month", span: time.Duration(*days) * 24 * time.Hour},
	} {
		start := now.Add(-c.span)
		plan, err := signals.Compile(signals.Context{OrganizationID: org, ProjectID: project.String(), From: start, To: now, Now: now, Step: c.step, RequestedAt: nil}, signals.Request{Selection: signals.Selection{SensorID: sensor.String(), SignalID: signal.String(), Threshold: 0.5, MinimumLevel: nil}, Trigger: "stream", RunID: "", Filters: nil, GroupBy: []signals.Dimension{{Field: "", Attribute: "department"}}, Groups: 5, Points: 10000})
		if err != nil {
			return fmt.Errorf("compile benchmark case: %w", err)
		}
		measurement := result{Name: c.name, ColdMS: 0, P50MS: 0, P95MS: 0, ReadRows: 0, ReadBytes: 0, MemoryBytes: 0, OracleMatched: false, Explain: nil, Refusal: ""}
		query := plan.Groups
		explain := query.SQL + ", use_query_condition_cache = 0, use_skip_indexes_on_data_read = 0"
		explained, err := conn.Query(ctx, "EXPLAIN indexes = 1 "+explain, query.Args...)
		if err != nil {
			return fmt.Errorf("explain %s: %w", c.name, err)
		}
		for explained.Next() {
			var line string
			if err := explained.Scan(&line); err != nil {
				_ = explained.Close()
				return fmt.Errorf("scan index analysis: %w", err)
			}
			measurement.Explain = append(measurement.Explain, line)
		}
		if err := explained.Err(); err != nil {
			_ = explained.Close()
			return fmt.Errorf("read index analysis: %w", err)
		}
		if err := explained.Close(); err != nil {
			return fmt.Errorf("close index analysis: %w", err)
		}
		for _, reset := range []string{"SYSTEM DROP MARK CACHE", "SYSTEM DROP UNCOMPRESSED CACHE", "SYSTEM DROP QUERY CACHE"} {
			if err := conn.Exec(ctx, reset); err != nil {
				return fmt.Errorf("reset benchmark caches: %w", err)
			}
		}
		var timings []float64
		queryID := ""
		for iteration := 0; iteration <= *iterations; iteration++ {
			queryID = fmt.Sprintf("sigint-benchmark-%s-%d", c.name, iteration)
			begin := time.Now()
			rows, err := (&analytics.Plan{Dataset: signals.Dataset, Name: "signals.canonical", SQL: query.SQL, Args: query.Args, Columns: nil}).Run(clickhouse.Context(ctx, clickhouse.WithQueryID(queryID+"-groups")), conn)
			if err != nil {
				var exception *clickhouse.Exception
				if !errors.As(err, &exception) || (exception.Code != 159 && exception.Code != 158 && exception.Code != 241) {
					return fmt.Errorf("execute benchmark %s: %w", c.name, err)
				}
				measurement.Refusal = err.Error()
				break
			}
			keys := make([]string, 0, len(rows)-1)
			for _, row := range rows[1:] {
				key, ok := row["group_key"].(string)
				if !ok {
					return fmt.Errorf("benchmark group has no string key")
				}
				keys = append(keys, key)
			}
			series, err := plan.Series(keys)
			if err != nil {
				return fmt.Errorf("compile benchmark series: %w", err)
			}
			points, err := (&analytics.Plan{Dataset: signals.Dataset, Name: "signals.canonical", SQL: series.SQL, Args: series.Args, Columns: nil}).Run(clickhouse.Context(ctx, clickhouse.WithQueryID(queryID+"-series")), conn)
			if err != nil {
				return fmt.Errorf("read benchmark series: %w", err)
			}
			elapsed := float64(time.Since(begin).Microseconds()) / 1000
			var expectedAnswered, expectedMatches int64
			expectedPoints := make(map[string][2]int64)
			for event := 0; event < *perDay**days; event++ {
				at := from.Add(time.Duration(event*86400 / *perDay) * time.Second)
				if at.Before(start) || !at.Before(now) || event%100 == 0 {
					continue
				}
				expectedAnswered++
				key := fmt.Sprintf("team-%d|%s", event%5, signals.BucketStart(at, c.step).Format(time.RFC3339Nano))
				point := expectedPoints[key]
				point[0]++
				if event%4 == 0 {
					expectedMatches++
					point[1]++
				}
				expectedPoints[key] = point
			}
			if len(rows) == 0 || rows[0]["answered"] != expectedAnswered || rows[0]["matched_messages"] != expectedMatches {
				return fmt.Errorf("%s disagrees with latest-attempt oracle: expected answered=%d matched=%d, summary=%v", c.name, expectedAnswered, expectedMatches, rows)
			}
			for _, point := range points {
				var labels [][]any
				groupKey, ok := point["group_key"].(string)
				if !ok {
					return fmt.Errorf("benchmark point has no group key")
				}
				if err := json.Unmarshal([]byte(groupKey), &labels); err != nil {
					return fmt.Errorf("decode benchmark group: %w", err)
				}
				key := fmt.Sprintf("%s|%s", labels[0][1], point["time_bucket"])
				expected := expectedPoints[key]
				if point["answered"] != expected[0] || point["matched_messages"] != expected[1] {
					return fmt.Errorf("%s point disagrees with oracle", c.name)
				}
				delete(expectedPoints, key)
			}
			if len(expectedPoints) > 0 {
				return fmt.Errorf("%s omitted nonempty chart points", c.name)
			}
			measurement.OracleMatched = true
			if iteration == 0 {
				measurement.ColdMS = elapsed
			} else {
				timings = append(timings, elapsed)
			}
		}
		if len(timings) > 0 {
			slices.Sort(timings)
			measurement.P50MS = timings[len(timings)/2]
			measurement.P95MS = timings[len(timings)-1]
		}
		if err := conn.Exec(ctx, "SYSTEM FLUSH LOGS"); err != nil {
			return fmt.Errorf("flush benchmark diagnostics: %w", err)
		}
		if measurement.Refusal == "" {
			if err := conn.QueryRow(ctx, "SELECT sum(read_rows), sum(read_bytes), max(memory_usage) FROM system.query_log WHERE query_id IN (?, ?) AND type = 'QueryFinish'", queryID+"-groups", queryID+"-series").Scan(&measurement.ReadRows, &measurement.ReadBytes, &measurement.MemoryBytes); err != nil {
				return fmt.Errorf("read benchmark diagnostics: %w", err)
			}
		}
		r.Results = append(r.Results, measurement)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode benchmark report: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0700); err != nil {
		return fmt.Errorf("create benchmark report directory: %w", err)
	}
	if err := writeReportData(ctx, *out, append(data, '\n')); err != nil {
		return fmt.Errorf("write benchmark report: %w", err)
	}
	fmt.Printf("ClickHouse %s: %d logical readings, report %s\n", r.Version, total, *out)
	for _, measurement := range r.Results {
		fmt.Printf("%s: cold %.1fms, warm p50 %.1fms/p95 %.1fms, rows %d, oracle %t, refusal %s\n", measurement.Name, measurement.ColdMS, measurement.P50MS, measurement.P95MS, measurement.ReadRows, measurement.OracleMatched, measurement.Refusal)
	}
	return nil
}

// writeReportData replaces the report atomically without following a preexisting
// destination symlink. Each checkpoint is a complete, owner-readable file.
func writeReportData(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("report context: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sigint-report-*")
	if err != nil {
		return fmt.Errorf("create report checkpoint: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return os.Remove(file.Name()) })
	defer o11y.NoLogDefer(file.Close)
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write report checkpoint: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close report checkpoint: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace report checkpoint: %w", err)
	}
	return nil
}
