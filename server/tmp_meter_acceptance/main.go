package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

const (
	adminUser        = "bench"
	readerUser       = "meter_reader"
	labPassword      = "bench"
	maxMemoryUsage   = uint64(512 * 1024 * 1024)
	defaultMaxRows   = uint64(10_000_000)
	defaultMaxBytes  = uint64(2 * 1024 * 1024 * 1024)
	defaultQueryTime = 30 * time.Second
)

type endpoints struct {
	WriterNative string `json:"writer_native"`
	ReaderNative string `json:"reader_native"`
	WriterHTTP   string `json:"writer_http"`
	ReaderHTTP   string `json:"reader_http"`
	Database     string `json:"database"`
}

type options struct {
	mode           string
	scenario       string
	fault          string
	outputPath     string
	endpointsPath  string
	organizationID string
	fixturePath    string
	fixture        fixtureMetadata
	from           time.Time
	to             time.Time
	snapshotAt     time.Time
	deadline       time.Duration
	loopDuration   time.Duration
	readers        int
	repetitions    int
	expect         string
	maxRowsToRead  uint64
	maxBytesToRead uint64
}

type fixtureMetadata struct {
	StartNS        string       `json:"start_ns"`
	EndNS          string       `json:"end_ns"`
	From           string       `json:"from"`
	To             string       `json:"to"`
	OrganizationID string       `json:"organization_id"`
	LogicalRows    uint64       `json:"logical_rows"`
	LoadedRows     uint64       `json:"loaded_rows"`
	RateRows       uint64       `json:"rate_rows"`
	RateDurationNS string       `json:"rate_duration_ns"`
	ExactTotals    []exactTotal `json:"exact_totals"`
}

type exactTotal struct {
	From         string `json:"from"`
	To           string `json:"to"`
	Family       string `json:"family"`
	ReadingKind  string `json:"reading_kind"`
	Quantity     string `json:"quantity"`
	ReadingCount string `json:"reading_count"`
}

type report struct {
	Mode           string       `json:"mode"`
	Scenario       string       `json:"scenario,omitempty"`
	Database       string       `json:"database"`
	Endpoint       string       `json:"endpoint"`
	OrganizationID string       `json:"organization_id,omitempty"`
	From           *time.Time   `json:"from,omitempty"`
	To             *time.Time   `json:"to,omitempty"`
	StartedAt      time.Time    `json:"started_at"`
	FinishedAt     time.Time    `json:"finished_at"`
	Success        bool         `json:"success"`
	Cases          []caseReport `json:"cases"`
	Failures       []failure    `json:"failures,omitempty"`
}

type caseSpec struct {
	name        string
	requestKind string
	family      metering.UsageFamily
	breakdown   string
	readingKind string
	from        time.Time
	to          time.Time
}

type caseReport struct {
	Name                  string            `json:"name"`
	RequestKind           string            `json:"request_kind"`
	Family                string            `json:"family"`
	Breakdown             string            `json:"breakdown"`
	ReadingKind           string            `json:"reading_kind"`
	From                  time.Time         `json:"from"`
	To                    time.Time         `json:"to"`
	ExpectedOutcome       string            `json:"expected_outcome"`
	Samples               int               `json:"samples"`
	SuccessfulSamples     int               `json:"successful_samples"`
	UnavailableSamples    int               `json:"unavailable_samples"`
	FailedSamples         int               `json:"failed_samples"`
	ConservationChecks    int               `json:"conservation_checks"`
	ConservationReference string            `json:"conservation_reference,omitempty"`
	P50                   time.Duration     `json:"p50_ns"`
	P95                   time.Duration     `json:"p95_ns"`
	Max                   time.Duration     `json:"max_ns"`
	NativeProgressRows    uint64            `json:"native_progress_rows"`
	NativeProgressBytes   uint64            `json:"native_progress_bytes"`
	NativeProfileRows     uint64            `json:"native_profile_rows"`
	NativeProfileBytes    uint64            `json:"native_profile_bytes"`
	NativeProfileEvents   map[string]int64  `json:"native_profile_events,omitempty"`
	QueryLogReadRows      uint64            `json:"query_log_read_rows"`
	QueryLogReadBytes     uint64            `json:"query_log_read_bytes"`
	QueryLogPeakMemory    uint64            `json:"query_log_peak_memory"`
	QueryLogProfileEvents map[string]uint64 `json:"query_log_profile_events,omitempty"`
	Failures              []failure         `json:"failures,omitempty"`
	UnavailableDetails    []failure         `json:"unavailable_details,omitempty"`
}

type failure struct {
	Case       string `json:"case,omitempty"`
	Reader     int    `json:"reader,omitempty"`
	Repetition int    `json:"repetition,omitempty"`
	QueryID    string `json:"query_id,omitempty"`
	Kind       string `json:"kind"`
	Detail     string `json:"detail"`
}

type sample struct {
	reader        int
	repetition    int
	queryID       string
	duration      time.Duration
	outcome       string
	failure       *failure
	progressRows  uint64
	progressBytes uint64
	profileRows   uint64
	profileBytes  uint64
	profileEvents map[string]int64
}

type queryLogMetrics struct {
	readRows      uint64
	readBytes     uint64
	peakMemory    uint64
	selectedRows  uint64
	selectedBytes uint64
	realTimeUS    uint64
}

var sequence atomic.Uint64
var statementSequence atomic.Uint64
var runID = time.Now().UnixNano()

func main() {
	opts, err := parseOptions()
	if err != nil {
		emitError(err)
	}
	eps, err := loadEndpoints(opts.endpointsPath)
	if err != nil {
		emitError(err)
	}
	started := time.Now().UTC()
	result := report{
		Mode: opts.mode, Scenario: opts.scenario, Database: eps.Database,
		OrganizationID: opts.organizationID, StartedAt: started, Cases: []caseReport{}, Failures: []failure{},
	}
	if opts.mode == "build" {
		result.Endpoint = eps.WriterNative
		err = runBuild(opts, eps, &result)
	} else {
		result.Endpoint = eps.ReaderNative
		from, to := opts.from, opts.to
		result.From, result.To = &from, &to
		err = runReads(opts, eps, &result)
	}
	result.FinishedAt = time.Now().UTC()
	result.Success = err == nil && len(result.Failures) == 0
	if err != nil {
		result.Failures = append(result.Failures, failure{Kind: "harness", Detail: err.Error()})
		result.Success = false
	}
	output := os.Stdout
	if opts.outputPath != "" {
		file, err := os.Create(opts.outputPath)
		if err != nil {
			emitError(err)
		}
		defer file.Close()
		output = file
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if encodeErr := encoder.Encode(result); encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		os.Exit(2)
	}
	if !result.Success {
		os.Exit(1)
	}
}

func parseOptions() (options, error) {
	var fromText, toText, snapshotText string
	var opts options
	flag.StringVar(&opts.mode, "mode", "", "required: build or reads")
	flag.StringVar(&opts.scenario, "scenario", "matrix", "reads scenario: matrix, publication, or replica")
	flag.StringVar(&opts.fault, "fault", "", "isolated guard probe only: pause-before-exchange or late-after-first-stage")
	flag.StringVar(&opts.outputPath, "output", "", "write the structured report to this file")
	flag.StringVar(&opts.endpointsPath, "endpoints", ".playwright-cli/meter-acceptance-lab/endpoints.json", "lab endpoints JSON")
	flag.StringVar(&opts.organizationID, "org", "syntheticorg_meter_acceptance", "organization to read")
	flag.StringVar(&opts.fixturePath, "fixture", ".playwright-cli/meter-acceptance-lab/fixture.json", "synthetic fixture metadata used for default bounds and exact totals")
	flag.StringVar(&fromText, "from", "", "inclusive RFC3339 usage boundary; defaults from fixture metadata")
	flag.StringVar(&toText, "to", "", "exclusive RFC3339 usage boundary; defaults from fixture metadata")
	flag.StringVar(&snapshotText, "snapshot-at", "", "RFC3339 rebuild snapshot; defaults to current UTC time")
	flag.DurationVar(&opts.deadline, "deadline", defaultQueryTime, "deadline for each repository request and administrative query")
	flag.DurationVar(&opts.loopDuration, "loop-duration", 2*time.Minute, "publication/replica scenario run time")
	flag.IntVar(&opts.readers, "readers", 5, "concurrent repository readers")
	flag.IntVar(&opts.repetitions, "repetitions", 3, "requests per reader in matrix mode")
	flag.StringVar(&opts.expect, "expect", "available", "available, unavailable, or available-or-unavailable")
	flag.Uint64Var(&opts.maxRowsToRead, "max-rows-to-read", defaultMaxRows, "per-query scan row cap")
	flag.Uint64Var(&opts.maxBytesToRead, "max-bytes-to-read", defaultMaxBytes, "per-query scan byte cap")
	flag.Parse()

	if opts.mode != "build" && opts.mode != "reads" {
		return options{}, errors.New("-mode must be build or reads")
	}
	if opts.deadline <= 0 || opts.deadline > time.Hour {
		return options{}, errors.New("-deadline must be positive and at most 1h")
	}
	if opts.readers != 5 {
		return options{}, errors.New("-readers must be 5 for the acceptance run")
	}
	if opts.repetitions < 1 {
		return options{}, errors.New("-repetitions must be positive")
	}
	if opts.maxRowsToRead == 0 || opts.maxBytesToRead == 0 {
		return options{}, errors.New("query scan caps must be positive")
	}
	if opts.expect != "available" && opts.expect != "unavailable" && opts.expect != "available-or-unavailable" {
		return options{}, errors.New("-expect must be available, unavailable, or available-or-unavailable")
	}
	if opts.mode == "build" {
		if snapshotText == "" {
			opts.snapshotAt = time.Now().UTC()
		} else {
			parsed, err := time.Parse(time.RFC3339Nano, snapshotText)
			if err != nil {
				return options{}, fmt.Errorf("parse -snapshot-at: %w", err)
			}
			opts.snapshotAt = parsed.UTC()
		}
		return opts, nil
	}
	if opts.scenario != "matrix" && opts.scenario != "publication" && opts.scenario != "replica" {
		return options{}, errors.New("-scenario must be matrix, publication, or replica")
	}
	fixture, fixtureErr := loadFixture(opts.fixturePath)
	if fixtureErr == nil {
		opts.fixture = fixture
		if fromText == "" {
			fromText = fixture.From
		}
		if toText == "" {
			toText = fixture.To
		}
		if opts.organizationID == "syntheticorg_meter_acceptance" && fixture.OrganizationID != "" {
			opts.organizationID = fixture.OrganizationID
		}
	} else if fromText == "" || toText == "" || opts.organizationID == "syntheticorg_meter_acceptance" {
		return options{}, fmt.Errorf("explicit -from/-to/-org required when fixture metadata is unavailable: %w", fixtureErr)
	}
	if fromText == "" || toText == "" {
		return options{}, errors.New("-from and -to are required when fixture metadata has no defaults")
	}
	parsedFrom, err := time.Parse(time.RFC3339Nano, fromText)
	if err != nil {
		return options{}, fmt.Errorf("parse -from: %w", err)
	}
	parsedTo, err := time.Parse(time.RFC3339Nano, toText)
	if err != nil {
		return options{}, fmt.Errorf("parse -to: %w", err)
	}
	opts.from, opts.to = parsedFrom.UTC(), parsedTo.UTC()
	if threeMonths := opts.to.AddDate(0, -3, 0); opts.from.Before(threeMonths) && fromText == fixture.From {
		opts.from = threeMonths
	}
	if !opts.from.Before(opts.to) {
		return options{}, errors.New("-from must precede -to")
	}
	if opts.to.After(opts.from.AddDate(0, 3, 0)) {
		return options{}, errors.New("requested window exceeds three calendar months")
	}
	if opts.scenario != "matrix" && opts.loopDuration <= 0 {
		return options{}, errors.New("-loop-duration must be positive for loop scenarios")
	}
	return opts, nil
}

func loadFixture(path string) (fixtureMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fixtureMetadata{}, err
	}
	var fixture fixtureMetadata
	if err := json.Unmarshal(data, &fixture); err != nil {
		return fixtureMetadata{}, fmt.Errorf("decode fixture metadata: %w", err)
	}
	if fixture.From == "" || fixture.To == "" {
		return fixtureMetadata{}, errors.New("fixture metadata must contain from and to")
	}
	from, err := time.Parse(time.RFC3339Nano, fixture.From)
	if err != nil {
		return fixtureMetadata{}, fmt.Errorf("parse fixture from: %w", err)
	}
	to, err := time.Parse(time.RFC3339Nano, fixture.To)
	if err != nil {
		return fixtureMetadata{}, fmt.Errorf("parse fixture to: %w", err)
	}
	startNS, err := strconv.ParseInt(fixture.StartNS, 10, 64)
	if err != nil {
		return fixtureMetadata{}, fmt.Errorf("parse fixture start_ns: %w", err)
	}
	endNS, err := strconv.ParseInt(fixture.EndNS, 10, 64)
	if err != nil {
		return fixtureMetadata{}, fmt.Errorf("parse fixture end_ns: %w", err)
	}
	if startNS != from.UnixNano() || endNS != to.UnixNano() || !from.Before(to) {
		return fixtureMetadata{}, errors.New("fixture nanosecond and RFC3339 bounds disagree")
	}
	if fixture.RateRows != 29_000_000 || fixture.RateDurationNS != "604800000000000" {
		return fixtureMetadata{}, errors.New("fixture rate does not match the accepted synthetic formula")
	}
	if fixture.LoadedRows < fixture.LogicalRows {
		return fixtureMetadata{}, errors.New("fixture loaded_rows cannot be less than logical_rows")
	}
	return fixture, nil
}

func loadEndpoints(path string) (endpoints, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return endpoints{}, fmt.Errorf("read endpoints: %w", err)
	}
	var eps endpoints
	if err := json.Unmarshal(data, &eps); err != nil {
		return endpoints{}, fmt.Errorf("decode endpoints: %w", err)
	}
	if eps.WriterNative == "" || eps.ReaderNative == "" || eps.WriterHTTP == "" || eps.ReaderHTTP == "" || eps.Database == "" {
		return endpoints{}, errors.New("endpoints JSON must contain writer_native, reader_native, writer_http, reader_http, and database")
	}
	for name, address := range map[string]string{"writer_native": eps.WriterNative, "reader_native": eps.ReaderNative} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return endpoints{}, fmt.Errorf("invalid %s endpoint: %w", name, err)
		}
	}
	return eps, nil
}

func openConn(address, database, username string, maxOpen int, opts options) (clickhouse.Conn, error) {
	settings := clickhouse.Settings{
		"max_memory_usage":                        maxMemoryUsage,
		"max_rows_to_read":                        opts.maxRowsToRead,
		"max_bytes_to_read":                       opts.maxBytesToRead,
		"timeout_before_checking_execution_speed": 0,
	}
	if opts.fault == "pause-before-exchange" {
		settings["distributed_ddl_task_timeout"] = 2
	}
	conn, err := clickhouse.Open(&clickhouse.Options{
		Protocol:        clickhouse.Native,
		Addr:            []string{address},
		Auth:            clickhouse.Auth{Database: database, Username: username, Password: labPassword},
		Settings:        settings,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     opts.deadline,
		MaxOpenConns:    maxOpen,
		MaxIdleConns:    maxOpen,
		ConnMaxLifetime: 5 * time.Minute,
		Compression:     &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.deadline)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping %s as %s: %w", address, username, err)
	}
	return &observedConn{Conn: conn}, nil
}

type requestTagKey struct{}

type observedConn struct{ clickhouse.Conn }

func statementContext(ctx context.Context) context.Context {
	tag, _ := ctx.Value(requestTagKey{}).(string)
	if tag == "" {
		return ctx
	}
	return clickhouse.Context(ctx, clickhouse.WithQueryID(fmt.Sprintf("%s~%d", tag, statementSequence.Add(1))))
}

func (conn *observedConn) Exec(ctx context.Context, query string, args ...any) error {
	return conn.Conn.Exec(statementContext(ctx), query, args...)
}

func (conn *observedConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	return conn.Conn.Query(statementContext(ctx), query, args...)
}

func (conn *observedConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	return conn.Conn.QueryRow(statementContext(ctx), query, args...)
}

type faultConn struct {
	clickhouse.Conn
	mode     string
	injected bool
}

func (conn *faultConn) Exec(ctx context.Context, query string, args ...any) error {
	if conn.mode == "pause-before-exchange" && strings.HasPrefix(strings.TrimSpace(query), "EXCHANGE TABLES") {
		if output, err := exec.CommandContext(ctx, "docker", "pause", "gram-meter-acceptance-reader-1").CombinedOutput(); err != nil {
			return fmt.Errorf("pause reader: %s: %w", output, err)
		}
	}
	err := conn.Conn.Exec(ctx, query, args...)
	if err == nil && conn.mode == "late-after-first-stage" && !conn.injected &&
		strings.Contains(query, "INSERT INTO billing_meter_daily_summaries_staging") {
		conn.injected = true
		return conn.Conn.Exec(ctx, `
			INSERT INTO billing_meter_readings_by_time
				(id, organization_id, project_id, meter_id, operation_id, unit, value, occurred_at, produced_at, attributes)
			VALUES ('00000000-0000-4000-8000-000000000005', 'org_meter_replica_guard',
				'00000000-0000-4000-8000-000000000010', 'gram.agent_session.storage',
				'late-during-build', 'stokens', 5, '2026-07-31 15:00:00', now64(9), map('model', 'late'))
		`)
	}
	return err
}

func runBuild(opts options, eps endpoints, result *report) error {
	conn, err := openConn(eps.WriterNative, eps.Database, adminUser, 2, opts)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if opts.fault != "" {
		if eps.Database != "meter_replica_guard" ||
			(opts.fault != "pause-before-exchange" && opts.fault != "late-after-first-stage") {
			return errors.New("fault injection requires the isolated meter_replica_guard database")
		}
		conn = &faultConn{Conn: conn, mode: opts.fault}
	}
	queryID := fmt.Sprintf("meter-acceptance-build-%d", time.Now().UnixNano())
	stats := &sample{queryID: queryID, profileEvents: map[string]int64{}}
	ctx, cancel := instrumentedContext(context.Background(), opts, queryID, stats)
	start := time.Now()
	err = chrepo.New(conn).RebuildUsageSummaries(ctx, opts.snapshotAt)
	stats.duration = time.Since(start)
	cancel()
	caseResult := caseReport{
		Name: "summary_rebuild", RequestKind: "summary_rebuild", ExpectedOutcome: "available",
		Samples: 1, P50: stats.duration, P95: stats.duration, Max: stats.duration,
		NativeProgressRows: stats.progressRows, NativeProgressBytes: stats.progressBytes,
		NativeProfileRows: stats.profileRows, NativeProfileBytes: stats.profileBytes,
		NativeProfileEvents: stats.profileEvents, QueryLogProfileEvents: map[string]uint64{}, Failures: []failure{},
	}
	if err != nil {
		caseResult.FailedSamples = 1
		caseResult.Failures = append(caseResult.Failures, failure{Case: caseResult.Name, QueryID: queryID, Kind: classifyError(err), Detail: err.Error()})
	} else {
		caseResult.SuccessfulSamples = 1
	}
	metrics, metricErr := collectQueryLog(context.Background(), conn, opts, []string{queryID})
	if metricErr != nil {
		caseResult.Failures = append(caseResult.Failures, failure{Case: caseResult.Name, QueryID: queryID, Kind: "query_log", Detail: metricErr.Error()})
	} else {
		mergeQueryLog(&caseResult, metrics)
	}
	result.Cases = append(result.Cases, caseResult)
	if len(caseResult.Failures) != 0 {
		result.Failures = append(result.Failures, caseResult.Failures...)
		return errors.New("summary rebuild acceptance failed")
	}
	return nil
}

func runReads(opts options, eps endpoints, result *report) error {
	reader, err := openConn(eps.ReaderNative, eps.Database, readerUser, opts.readers, opts)
	if err != nil {
		if opts.scenario == "replica" && opts.expect != "available" {
			result.Cases = append(result.Cases, unavailableConnectionCase(opts, err))
			return nil
		}
		return err
	}
	defer func() { _ = reader.Close() }()
	admin, err := openConn(eps.ReaderNative, eps.Database, adminUser, opts.readers+1, opts)
	if err != nil {
		if opts.scenario != "replica" || opts.expect == "available" {
			return fmt.Errorf("open reader-node administrative connection: %w", err)
		}
		admin = nil
	} else {
		defer func() { _ = admin.Close() }()
	}

	var publication time.Time
	if opts.scenario == "matrix" {
		publication, err = readPublication(context.Background(), admin, opts)
		if err != nil {
			return err
		}
	}
	cases, err := makeCases(opts, publication)
	if err != nil {
		return err
	}
	for _, spec := range cases {
		caseResult := runReadCase(opts, reader, admin, spec)
		result.Cases = append(result.Cases, caseResult)
		if len(caseResult.Failures) != 0 {
			result.Failures = append(result.Failures, caseResult.Failures...)
		}
	}
	if len(result.Failures) != 0 {
		return errors.New("one or more read acceptance cases failed")
	}
	return nil
}

func unavailableConnectionCase(opts options, err error) caseReport {
	detail := failure{Case: "replica_connection", Kind: "unavailable", Detail: err.Error()}
	caseResult := caseReport{
		Name: "replica_connection", RequestKind: "replica_outage", ExpectedOutcome: opts.expect,
		Samples: opts.readers, UnavailableSamples: opts.readers, NativeProfileEvents: map[string]int64{},
		QueryLogProfileEvents: map[string]uint64{}, Failures: []failure{},
		UnavailableDetails: []failure{detail},
	}
	if opts.expect == "available" {
		caseResult.FailedSamples = opts.readers
		caseResult.Failures = append(caseResult.Failures, detail)
	}
	return caseResult
}

func readPublication(parent context.Context, conn clickhouse.Conn, opts options) (time.Time, error) {
	ctx, cancel := context.WithTimeout(parent, opts.deadline)
	defer cancel()
	ctx = clickhouse.Context(ctx, clickhouse.WithQueryID(fmt.Sprintf("meter-acceptance-publication-%d", time.Now().UnixNano())))
	var publishedBefore, snapshotAt time.Time
	err := conn.QueryRow(ctx, `
		SELECT argMax(published_before, snapshot_at), max(snapshot_at)
		FROM billing_meter_daily_summaries
		WHERE organization_id = '' AND is_publication = 1 AND family = '' AND reading_kind = '' AND facet = ''
		LIMIT 1
		SETTINGS max_execution_time = 30, max_rows_to_read = 1000, max_bytes_to_read = 1048576, timeout_before_checking_execution_speed = 0
	`).Scan(&publishedBefore, &snapshotAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("read summary publication: %w", err)
	}
	if snapshotAt.IsZero() {
		return time.Time{}, chrepo.ErrUsageSummaryUnavailable
	}
	return publishedBefore.UTC(), nil
}

func makeCases(opts options, publication time.Time) ([]caseSpec, error) {
	if opts.scenario == "publication" || opts.scenario == "replica" {
		return []caseSpec{{
			name: opts.scenario + "_probe", requestKind: opts.scenario + "_loop",
			family: metering.UsageFamilyAgentSessionStorage, breakdown: "billing_user",
			readingKind: chrepo.ReadingKindUsage, from: opts.from, to: opts.to,
		}}, nil
	}
	selections := []struct {
		family     metering.UsageFamily
		breakdowns []string
	}{
		{metering.UsageFamilyAgentSessionStorage, []string{"total", "project", "model", "provider", "billing_mode", "assistant", "billing_user", "division", "department", "job_title", "employee_type", "cost_center", "directory_group_set"}},
		{metering.UsageFamilyMCPBandwidth, []string{"total", "project", "direction", "mcp_server", "server_type"}},
		{metering.UsageFamilyRiskContentScans, []string{"total", "project", "scanner", "policy", "judge_model", "judge_provider", "tool_name"}},
	}
	requestKind := "bounded_window"
	if opts.to.Equal(opts.from.AddDate(0, 3, 0)) {
		requestKind = "max_three_calendar_month"
	}
	cases := make([]caseSpec, 0, 206)
	for _, selection := range selections {
		for _, breakdown := range selection.breakdowns {
			for _, kind := range []string{chrepo.ReadingKindUsage, chrepo.ReadingKindAdjustment} {
				cases = append(cases, caseSpec{
					name:        string(selection.family) + "/" + breakdown + "/" + kind,
					requestKind: requestKind, family: selection.family, breakdown: breakdown,
					readingKind: kind, from: opts.from, to: opts.to,
				})
				monthFrom := firstFullMonthStart(opts.from)
				cases = append(cases, caseSpec{
					name:        "one_cycle/" + string(selection.family) + "/" + breakdown + "/" + kind,
					requestKind: "one_calendar_month", family: selection.family, breakdown: breakdown,
					readingKind: kind, from: monthFrom, to: monthFrom.AddDate(0, 1, 0),
				})
				partialFrom := monthFrom.Add(opts.from.Sub(opts.from.Truncate(24 * time.Hour)))
				cases = append(cases, caseSpec{
					name:        "partial_cycle/" + string(selection.family) + "/" + breakdown + "/" + kind,
					requestKind: "one_calendar_month_partial", family: selection.family, breakdown: breakdown,
					readingKind: kind, from: partialFrom, to: partialFrom.AddDate(0, 1, 0),
				})
				currentFrom := time.Date(opts.to.Year(), opts.to.Month(), 1, 0, 0, 0, 0, time.UTC)
				cases = append(cases, caseSpec{
					name:        "current_cycle/" + string(selection.family) + "/" + breakdown + "/" + kind,
					requestKind: "current_cycle", family: selection.family, breakdown: breakdown,
					readingKind: kind, from: currentFrom, to: opts.to,
				})
			}
		}
	}
	completedTo := opts.to
	if publication.Before(completedTo) {
		completedTo = publication
	}
	if !opts.from.Before(completedTo) {
		return nil, fmt.Errorf("completed-history case has no interval before publication %s", publication.Format(time.RFC3339Nano))
	}
	monthFrom := firstFullMonthStart(opts.from)
	monthTo := monthFrom.AddDate(0, 1, 0)
	if monthTo.After(opts.to) {
		return nil, errors.New("requested range does not contain one complete calendar month")
	}
	if !opts.from.Before(publication) || !publication.Before(opts.to) {
		return nil, fmt.Errorf("requested range must straddle publication %s for the mixed-tail case", publication.Format(time.RFC3339Nano))
	}
	mixedFrom := publication.AddDate(0, 0, -1)
	if opts.from.After(mixedFrom) {
		mixedFrom = opts.from
	}
	for _, kind := range []string{chrepo.ReadingKindUsage, chrepo.ReadingKindAdjustment} {
		cases = append(cases,
			caseSpec{name: "completed_history/agent_total/" + kind, requestKind: "completed_history", family: metering.UsageFamilyAgentSessionStorage, breakdown: "total", readingKind: kind, from: opts.from.Truncate(24 * time.Hour), to: completedTo},
			caseSpec{name: "mixed_tail/agent_model/" + kind, requestKind: "mixed_tail", family: metering.UsageFamilyAgentSessionStorage, breakdown: "model", readingKind: kind, from: mixedFrom, to: opts.to},
			caseSpec{name: "one_cycle/agent_directory_groups/" + kind, requestKind: "one_calendar_month", family: metering.UsageFamilyAgentSessionStorage, breakdown: "directory_group_set", readingKind: kind, from: monthFrom, to: monthTo},
		)
	}
	return cases, nil
}

func firstFullMonthStart(from time.Time) time.Time {
	from = from.UTC()
	start := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	if from.After(start) {
		start = start.AddDate(0, 1, 0)
	}
	return start
}

func runReadCase(opts options, reader, admin clickhouse.Conn, spec caseSpec) caseReport {
	result := caseReport{
		Name: spec.name, RequestKind: spec.requestKind, Family: string(spec.family), Breakdown: spec.breakdown,
		ReadingKind: spec.readingKind, From: spec.from, To: spec.to, ExpectedOutcome: opts.expect,
		NativeProfileEvents: map[string]int64{}, QueryLogProfileEvents: map[string]uint64{}, Failures: []failure{},
	}
	_, selection, err := metering.ResolveUsageSelection(spec.family, spec.breakdown)
	if err != nil {
		result.Failures = append(result.Failures, failure{Case: spec.name, Kind: "selection", Detail: err.Error()})
		return result
	}
	_, totalSelection, err := metering.ResolveUsageSelection(spec.family, "total")
	if err != nil {
		result.Failures = append(result.Failures, failure{Case: spec.name, Kind: "selection", Detail: err.Error()})
		return result
	}
	var canonical map[string]string
	var canonicalErr error
	if admin == nil {
		if expected, ok := fixtureExactTotal(opts.fixture, opts.organizationID, spec); ok {
			canonical = map[string]string{"*": expected.Quantity}
		} else {
			canonicalErr = errors.New("reader-node administrative connection is unavailable and fixture has no exact total")
		}
	} else {
		canonical, canonicalErr = canonicalDailyTotals(context.Background(), admin, opts, totalSelection, spec)
	}
	if _, ok := fixtureExactTotal(opts.fixture, opts.organizationID, spec); ok {
		result.ConservationReference = "fixture_exact_total"
	} else {
		result.ConservationReference = "bounded_canonical_query"
	}
	allowedTotals := make([]map[string]string, 0, 2)
	if canonicalErr == nil {
		allowedTotals = append(allowedTotals, canonical)
	}
	if opts.scenario != "matrix" && opts.expect != "unavailable" {
		baselineCtx, cancel := context.WithTimeout(context.Background(), opts.deadline)
		baseline, baselineErr := chrepo.New(reader).GetUsage(baselineCtx, chrepo.UsageParams{
			OrganizationID: opts.organizationID, Selection: totalSelection, From: spec.from, To: spec.to, ReadingKind: spec.readingKind,
		})
		cancel()
		if baselineErr == nil {
			baselineTotals, totalsErr := dailyTotalsFromRows(baseline.Rows)
			if totalsErr != nil {
				result.Failures = append(result.Failures, failure{Case: spec.name, Kind: "baseline", Detail: totalsErr.Error()})
				return result
			}
			allowedTotals = append(allowedTotals, baselineTotals)
		} else if opts.expect == "available" {
			result.Failures = append(result.Failures, failure{Case: spec.name, Kind: classifyError(baselineErr), Detail: baselineErr.Error()})
			return result
		}
	}
	if len(allowedTotals) == 0 && opts.expect == "available" {
		result.Failures = append(result.Failures, failure{Case: spec.name, Kind: "canonical_reference", Detail: canonicalErr.Error()})
		return result
	}

	repetitions := opts.repetitions
	var stopAt time.Time
	if opts.scenario != "matrix" {
		repetitions = 0
		stopAt = time.Now().Add(opts.loopDuration)
	}
	samples := make(chan sample, opts.readers*max(1, opts.repetitions))
	var wg sync.WaitGroup
	for readerIndex := range opts.readers {
		wg.Add(1)
		go func(readerIndex int) {
			defer wg.Done()
			for repetition := 0; repetitions == 0 || repetition < repetitions; repetition++ {
				if repetitions == 0 && repetition > 0 && time.Now().After(stopAt) {
					return
				}
				samples <- executeRead(opts, reader, selection, totalSelection, allowedTotals, spec, readerIndex, repetition)
			}
		}(readerIndex)
	}
	go func() {
		wg.Wait()
		close(samples)
	}()

	all := make([]sample, 0, opts.readers*max(1, opts.repetitions))
	for measured := range samples {
		all = append(all, measured)
	}
	queryIDs := make([]string, 0, len(all))
	durations := make([]time.Duration, 0, len(all))
	for _, measured := range all {
		result.Samples++
		queryIDs = append(queryIDs, measured.queryID)
		result.NativeProgressRows += measured.progressRows
		result.NativeProgressBytes += measured.progressBytes
		result.NativeProfileRows += measured.profileRows
		result.NativeProfileBytes += measured.profileBytes
		for name, value := range measured.profileEvents {
			result.NativeProfileEvents[name] += value
		}
		switch measured.outcome {
		case "success":
			result.SuccessfulSamples++
			result.ConservationChecks++
			durations = append(durations, measured.duration)
		case "unavailable":
			result.UnavailableSamples++
			if measured.failure != nil {
				result.UnavailableDetails = append(result.UnavailableDetails, *measured.failure)
			}
		default:
			result.FailedSamples++
		}
		if measured.failure != nil && measured.outcome != "unavailable" {
			result.Failures = append(result.Failures, *measured.failure)
		}
	}
	if len(durations) > 0 {
		slices.Sort(durations)
		result.P50 = percentile(durations, 50)
		result.P95 = percentile(durations, 95)
		result.Max = durations[len(durations)-1]
	}
	if err := enforceExpectedOutcome(opts.expect, result.SuccessfulSamples, result.UnavailableSamples, result.FailedSamples); err != nil {
		result.Failures = append(result.Failures, failure{Case: spec.name, Kind: "expectation", Detail: err.Error()})
	}
	if admin != nil {
		metrics, err := collectQueryLog(context.Background(), admin, opts, queryIDs)
		if err != nil {
			if opts.expect == "available" {
				result.Failures = append(result.Failures, failure{Case: spec.name, Kind: "query_log", Detail: err.Error()})
			}
		} else {
			mergeQueryLog(&result, metrics)
		}
	}
	return result
}

func executeRead(opts options, reader clickhouse.Conn, selection, totalSelection chrepo.UsageSelection, allowedTotals []map[string]string, spec caseSpec, readerIndex, repetition int) sample {
	queryID := fmt.Sprintf("meter-acceptance-%d-%s-%d-%d-%d", runID, sanitize(spec.name), readerIndex, repetition, sequence.Add(1))
	measured := sample{reader: readerIndex, repetition: repetition, queryID: queryID, profileEvents: map[string]int64{}}
	ctx, cancel := instrumentedContext(context.Background(), opts, queryID, &measured)
	start := time.Now()
	usage, err := chrepo.New(reader).GetUsage(ctx, chrepo.UsageParams{
		OrganizationID: opts.organizationID, Selection: selection, From: spec.from, To: spec.to, ReadingKind: spec.readingKind,
	})
	measured.duration = time.Since(start)
	cancel()
	if err == nil {
		err = validateUsageResult(usage, selection, spec)
	}
	if err != nil {
		measured.outcome = "unavailable"
		measured.failure = &failure{Case: spec.name, Reader: readerIndex, Repetition: repetition, QueryID: queryID, Kind: classifyError(err), Detail: err.Error()}
		if opts.expect == "available" || !isAvailabilityError(err) {
			measured.outcome = "failed"
		}
		return measured
	}
	if opts.expect == "unavailable" {
		measured.outcome = "failed"
		measured.failure = &failure{Case: spec.name, Reader: readerIndex, Repetition: repetition, QueryID: queryID, Kind: "unexpected_success", Detail: "repository returned a result while unavailability was required"}
		return measured
	}
	if len(allowedTotals) == 0 {
		measured.outcome = "failed"
		measured.failure = &failure{Case: spec.name, Reader: readerIndex, Repetition: repetition, QueryID: queryID, Kind: "canonical_reference", Detail: "no complete baseline or bounded canonical reference is available"}
		return measured
	}
	rows := usage.Rows
	if err := compareAnyDailyTotals(rows, allowedTotals); err != nil {
		measured.outcome = "failed"
		measured.failure = &failure{Case: spec.name, Reader: readerIndex, Repetition: repetition, QueryID: queryID, Kind: "conservation", Detail: err.Error()}
		return measured
	}
	measured.outcome = "success"
	return measured
}

func instrumentedContext(parent context.Context, opts options, queryID string, measured *sample) (context.Context, context.CancelFunc) {
	parent = context.WithValue(parent, requestTagKey{}, queryID)
	ctx, cancel := context.WithTimeout(parent, opts.deadline)
	ctx = clickhouse.Context(ctx,
		clickhouse.WithSettings(clickhouse.Settings{
			"log_comment":                             queryID,
			"max_memory_usage":                        maxMemoryUsage,
			"max_rows_to_read":                        opts.maxRowsToRead,
			"max_bytes_to_read":                       opts.maxBytesToRead,
			"max_execution_time":                      max(1, int(opts.deadline/time.Second)),
			"timeout_before_checking_execution_speed": 0,
		}),
		clickhouse.WithProgress(func(progress *clickhouse.Progress) {
			measured.progressRows += progress.Rows
			measured.progressBytes += progress.Bytes
		}),
		clickhouse.WithProfileInfo(func(info *clickhouse.ProfileInfo) {
			measured.profileRows += info.Rows
			measured.profileBytes += info.Bytes
		}),
		clickhouse.WithProfileEvents(func(events []clickhouse.ProfileEvent) {
			for _, event := range events {
				measured.profileEvents[event.Name] += event.Value
			}
		}),
	)
	return ctx, cancel
}

func fixtureExactTotal(fixture fixtureMetadata, organizationID string, spec caseSpec) (exactTotal, bool) {
	if fixture.OrganizationID == "" || fixture.OrganizationID != organizationID {
		return exactTotal{}, false
	}
	for _, expected := range fixture.ExactTotals {
		from, fromErr := time.Parse(time.RFC3339Nano, expected.From)
		to, toErr := time.Parse(time.RFC3339Nano, expected.To)
		if fromErr == nil && toErr == nil &&
			from.UTC().Equal(spec.from) && to.UTC().Equal(spec.to) &&
			expected.Family == string(spec.family) && expected.ReadingKind == spec.readingKind {
			return expected, true
		}
	}
	return exactTotal{}, false
}

func canonicalDailyTotals(parent context.Context, admin clickhouse.Conn, opts options, selection chrepo.UsageSelection, spec caseSpec) (map[string]string, error) {
	if expected, ok := fixtureExactTotal(opts.fixture, opts.organizationID, spec); ok {
		if _, valid := new(big.Int).SetString(expected.Quantity, 10); !valid {
			return nil, fmt.Errorf("fixture exact total has invalid quantity %q", expected.Quantity)
		}
		if _, valid := new(big.Int).SetString(expected.ReadingCount, 10); !valid {
			return nil, fmt.Errorf("fixture exact total has invalid reading_count %q", expected.ReadingCount)
		}
		return map[string]string{"*": expected.Quantity}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(selection.MeterIDs)), ",")
	query := fmt.Sprintf(`
		SELECT toString(toDate(occurred_at, 'UTC')), toString(sum(toInt128(value)))
		FROM billing_meter_readings_by_time FINAL
		WHERE organization_id = ? AND meter_id IN (%s) AND reading_kind = ?
			AND occurred_at >= toDateTime64(?, 9, 'UTC') AND occurred_at < toDateTime64(?, 9, 'UTC')
		GROUP BY toDate(occurred_at, 'UTC')
		ORDER BY toDate(occurred_at, 'UTC')
		LIMIT 100
		SETTINGS do_not_merge_across_partitions_select_final = 1, max_threads = 2, max_final_threads = 2,
			max_memory_usage = %d, max_rows_to_read = %d, max_bytes_to_read = %d,
			max_execution_time = %d, timeout_before_checking_execution_speed = 0
	`, placeholders, maxMemoryUsage, opts.maxRowsToRead, opts.maxBytesToRead, max(1, int(opts.deadline/time.Second)))
	args := make([]any, 0, len(selection.MeterIDs)+4)
	args = append(args, opts.organizationID)
	for _, meterID := range selection.MeterIDs {
		args = append(args, meterID)
	}
	args = append(args, spec.readingKind, spec.from, spec.to)
	ctx, cancel := context.WithTimeout(parent, opts.deadline)
	defer cancel()
	ctx = clickhouse.Context(ctx, clickhouse.WithQueryID(fmt.Sprintf("meter-acceptance-reference-%d", sequence.Add(1))))
	rows, err := admin.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query bounded canonical daily totals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	totals := map[string]string{}
	for rows.Next() {
		var day, total string
		if err := rows.Scan(&day, &total); err != nil {
			return nil, fmt.Errorf("scan bounded canonical daily totals: %w", err)
		}
		totals[day] = total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read bounded canonical daily totals: %w", err)
	}
	return totals, nil
}

func validateUsageResult(result chrepo.UsageResult, selection chrepo.UsageSelection, spec caseSpec) error {
	if result.Unit != selection.Unit || result.MeasurementMethod != selection.MeasurementMethod {
		return fmt.Errorf(
			"measurement contract differs: got %s/%s, expected %s/%s",
			result.Unit, result.MeasurementMethod, selection.Unit, selection.MeasurementMethod,
		)
	}
	rowsPerDay := make(map[string]int)
	series := make(map[string]struct{})
	for _, row := range result.Rows {
		series[row.Kind+"\x00"+row.Key] = struct{}{}
		if len(series) > 7 {
			return errors.New("result changes the selected top-six series across days")
		}
		if row.Day.Before(spec.from.Truncate(24*time.Hour)) || !row.Day.Before(spec.to) {
			return fmt.Errorf("result day %s is outside request range", row.Day.Format(time.RFC3339Nano))
		}
		if row.Unit != selection.Unit || row.MeasurementMethod != selection.MeasurementMethod {
			return fmt.Errorf("row measurement contract differs on %s", row.Day.Format(time.DateOnly))
		}
		if _, ok := new(big.Int).SetString(row.Total, 10); !ok {
			return fmt.Errorf("row contains invalid exact integer %q", row.Total)
		}
		day := row.Day.UTC().Format(time.DateOnly)
		rowsPerDay[day]++
		if rowsPerDay[day] > 7 {
			return fmt.Errorf("result exceeds seven bounded series on %s", day)
		}
	}
	return nil
}

func dailyTotalsFromRows(rows []chrepo.UsageRow) (map[string]string, error) {
	totals := make(map[string]*big.Int)
	for _, row := range rows {
		day := row.Day.UTC().Format(time.DateOnly)
		value, ok := new(big.Int).SetString(row.Total, 10)
		if !ok {
			return nil, fmt.Errorf("invalid exact integer %q for %s", row.Total, day)
		}
		if totals[day] == nil {
			totals[day] = new(big.Int)
		}
		totals[day].Add(totals[day], value)
	}
	result := make(map[string]string, len(totals))
	for day, total := range totals {
		result[day] = total.String()
	}
	return result, nil
}

func compareAnyDailyTotals(rows []chrepo.UsageRow, expected []map[string]string) error {
	var mismatches []string
	for _, candidate := range expected {
		if err := compareDailyTotals(rows, candidate); err == nil {
			return nil
		} else {
			mismatches = append(mismatches, err.Error())
		}
	}
	return fmt.Errorf("result matches neither a complete baseline nor bounded canonical truth: %s", strings.Join(mismatches, "; "))
}

func compareDailyTotals(rows []chrepo.UsageRow, expected map[string]string) error {
	actualText, err := dailyTotalsFromRows(rows)
	if err != nil {
		return err
	}
	actual := make(map[string]*big.Int, len(actualText))
	for day, total := range actualText {
		actual[day], _ = new(big.Int).SetString(total, 10)
	}
	if expectedTotal, aggregate := expected["*"]; aggregate {
		sum := new(big.Int)
		for _, value := range actual {
			sum.Add(sum, value)
		}
		wanted, ok := new(big.Int).SetString(expectedTotal, 10)
		if !ok {
			return fmt.Errorf("canonical reference contains invalid aggregate integer %q", expectedTotal)
		}
		if sum.Cmp(wanted) != 0 {
			return fmt.Errorf("family total differs: got %s, expected %s", sum, wanted)
		}
		return nil
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("daily total day count differs: got %d, expected %d", len(actual), len(expected))
	}
	for day, expectedText := range expected {
		expectedValue, ok := new(big.Int).SetString(expectedText, 10)
		if !ok {
			return fmt.Errorf("canonical reference contains invalid integer %q for %s", expectedText, day)
		}
		actualValue, ok := actual[day]
		if !ok {
			return fmt.Errorf("daily total is missing %s", day)
		}
		if actualValue.Cmp(expectedValue) != 0 {
			return fmt.Errorf("daily total differs for %s: got %s, expected %s", day, actualValue, expectedValue)
		}
	}
	return nil
}

func collectQueryLog(parent context.Context, admin clickhouse.Conn, opts options, queryIDs []string) (map[string]queryLogMetrics, error) {
	metrics := make(map[string]queryLogMetrics, len(queryIDs))
	if len(queryIDs) == 0 {
		return metrics, nil
	}
	ctx, cancel := context.WithTimeout(parent, opts.deadline)
	defer cancel()
	if err := admin.Exec(ctx, "SYSTEM FLUSH LOGS"); err != nil {
		return nil, fmt.Errorf("flush query log: %w", err)
	}
	for offset := 0; offset < len(queryIDs); offset += 100 {
		end := min(offset+100, len(queryIDs))
		batch := queryIDs[offset:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		query := fmt.Sprintf(`
			SELECT splitByChar('~', query_id)[1] AS request_tag, sum(read_rows), sum(read_bytes), max(memory_usage),
				sum(ProfileEvents['SelectedRows']), sum(ProfileEvents['SelectedBytes']), sum(ProfileEvents['RealTimeMicroseconds'])
			FROM system.query_log
			WHERE event_date >= today() - 1 AND request_tag IN (%s)
				AND type IN ('QueryFinish', 'ExceptionWhileProcessing', 'ExceptionBeforeStart')
			GROUP BY request_tag
			ORDER BY request_tag
			LIMIT 100
			SETTINGS max_execution_time = 30, max_rows_to_read = 1000000, max_bytes_to_read = 536870912,
				timeout_before_checking_execution_speed = 0
		`, placeholders)
		args := make([]any, len(batch))
		for index, queryID := range batch {
			args[index] = queryID
		}
		rows, err := admin.Query(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("query system.query_log: %w", err)
		}
		for rows.Next() {
			var queryID string
			var item queryLogMetrics
			if err := rows.Scan(&queryID, &item.readRows, &item.readBytes, &item.peakMemory, &item.selectedRows, &item.selectedBytes, &item.realTimeUS); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan system.query_log: %w", err)
			}
			metrics[queryID] = item
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("read system.query_log: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close system.query_log: %w", err)
		}
	}
	if opts.expect == "available" {
		for _, queryID := range queryIDs {
			if _, ok := metrics[queryID]; !ok {
				return nil, fmt.Errorf("query log is missing request tag %s", queryID)
			}
		}
	}
	return metrics, nil
}

func mergeQueryLog(result *caseReport, metrics map[string]queryLogMetrics) {
	for _, item := range metrics {
		result.QueryLogReadRows += item.readRows
		result.QueryLogReadBytes += item.readBytes
		result.QueryLogPeakMemory = max(result.QueryLogPeakMemory, item.peakMemory)
		result.QueryLogProfileEvents["SelectedRows"] += item.selectedRows
		result.QueryLogProfileEvents["SelectedBytes"] += item.selectedBytes
		result.QueryLogProfileEvents["RealTimeMicroseconds"] += item.realTimeUS
	}
}

func enforceExpectedOutcome(expect string, successes, unavailable, failed int) error {
	if failed != 0 {
		return fmt.Errorf("%d requests failed validation", failed)
	}
	switch expect {
	case "available":
		if unavailable != 0 || successes == 0 {
			return fmt.Errorf("expected only available exact results; got %d successes and %d unavailable", successes, unavailable)
		}
	case "unavailable":
		if successes != 0 || unavailable == 0 {
			return fmt.Errorf("expected only explicit unavailability; got %d successes and %d unavailable", successes, unavailable)
		}
	case "available-or-unavailable":
		if successes+unavailable == 0 {
			return errors.New("no read attempts completed")
		}
	}
	return nil
}

func classifyError(err error) string {
	if errors.Is(err, chrepo.ErrUsageSummaryUnavailable) {
		return "usage_summary_unavailable"
	}
	if exception, ok := errors.AsType[*clickhouse.Exception](err); ok {
		return fmt.Sprintf("clickhouse_%d", exception.Code)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "unavailable"
}

func isAvailabilityError(err error) bool {
	if errors.Is(err, chrepo.ErrUsageSummaryUnavailable) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, clickhouse.ErrConnectionClosed) ||
		errors.Is(err, clickhouse.ErrAcquireConnTimeout) ||
		errors.Is(err, clickhouse.ErrAcquireConnNoAddress) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func percentile(sorted []time.Duration, percent int) time.Duration {
	index := max((len(sorted)*percent+99)/100, 1)
	return sorted[index-1]
}

func sanitize(value string) string {
	replacer := strings.NewReplacer("/", "-", "_", "-", " ", "-")
	return replacer.Replace(value)
}

func emitError(err error) {
	_ = json.NewEncoder(os.Stdout).Encode(report{Mode: "invalid", Success: false, Failures: []failure{{Kind: "configuration", Detail: err.Error()}}})
	os.Exit(2)
}
