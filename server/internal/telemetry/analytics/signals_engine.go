package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"

	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

// SignalMetadata is the measurement and completeness contract shared by surfaces.
type SignalMetadata struct {
	// Selection records the contributing sensor and explicit matching rule.
	Selection signalquery.Selection `json:"selection"`

	// Trigger distinguishes streaming and manual populations.
	Trigger string `json:"trigger"`

	// RunID names the selected manual evaluation, empty for streaming.
	RunID string `json:"run_id,omitempty"`

	// Step is the effective UTC output interval.
	Step string `json:"step"`

	// PhysicalResolution identifies what was actually read, independently of Step.
	PhysicalResolution string `json:"physical_resolution"`

	// TimeAxis names the source activity clock.
	TimeAxis string `json:"time_axis"`

	// Unit identifies one counted observation.
	Unit string `json:"unit"`

	// Population defines the rate denominator and its exclusions.
	Population string `json:"population"`

	// Coverage is unknown until an eligible-message census exists.
	Coverage string `json:"coverage"`

	// QueriedAt is query time, not a replayable historical snapshot.
	QueriedAt string `json:"queried_at"`

	// From is the inclusive activity-window boundary, including partial edges.
	From string `json:"from"`

	// To is the exclusive activity-window boundary.
	To string `json:"to"`

	// Filters retains the measured cohort for interpreting exported results.
	Filters []signalquery.Filter `json:"filters"`

	// Snapshot states that live latest-attempt reads may change on requery.
	Snapshot string `json:"snapshot"`

	// Exact applies to counts and distinct conversations, including whole-window totals.
	Exact bool `json:"exact"`

	// SeriesComplete means every bucket of each selected group is returned.
	SeriesComplete bool `json:"series_complete"`

	// GroupsTruncated indicates intentional whole-window top-group selection.
	GroupsTruncated bool `json:"groups_truncated"`

	// GroupBy interprets group_values in order, preserving absent versus empty labels.
	GroupBy []signalquery.Dimension `json:"group_by"`

	// PointBudget is the effective complete-series budget.
	PointBudget int `json:"point_budget"`
}

// SignalResult contains whole-window measures and complete selected time series.
type SignalResult struct {
	// Metadata preserves units, rule, clocks, freshness and completeness.
	Metadata SignalMetadata `json:"metadata"`

	// Summary is exact over the entire filtered window, including unselected groups.
	Summary Row `json:"summary"`

	// Groups contains whole-window totals for the selected series.
	Groups []Row `json:"groups"`

	// Rows contains all selected buckets, including explicitly unknown gaps.
	Rows []Row `json:"-"`
}

// SignalQuery executes the canonical semantic compiler through the shared runner.
func (e *Engine) SignalQuery(ctx context.Context, tenant Tenant, window Window, step string, request signalquery.Request) (*SignalResult, error) {
	plan, err := signalquery.Compile(signalquery.Context{
		OrganizationID: tenant.OrganizationID, ProjectID: tenant.ProjectID,
		From: time.Unix(0, window.FromUnixNano), To: time.Unix(0, window.ToUnixNano), Now: time.Now().UTC(), Step: step, RequestedAt: nil,
	}, request)
	if err != nil {
		return nil, signalFailure(err)
	}
	if plan.Request.Trigger == "manual" {
		scope, err := e.signalManualContext(ctx, plan.Context, plan.Request.RunID)
		if err != nil {
			return nil, err
		}
		plan, err = signalquery.Compile(scope, plan.Request)
		if err != nil {
			return nil, signalFailure(err)
		}
	}
	top, err := runSignalPlan(ctx, e.ch, plan.Groups)
	if err != nil {
		return nil, err
	}
	result := &SignalResult{
		Metadata: SignalMetadata{
			Selection: plan.Request.Selection, Trigger: plan.Request.Trigger, RunID: plan.Request.RunID,
			Step: plan.Context.Step, PhysicalResolution: "canonical", TimeAxis: "event_occurred_at", Unit: "original_message",
			Population: "answered original-message readings that measured the selected signal or ordered level",
			Coverage:   "unknown", QueriedAt: plan.Context.Now.Format(time.RFC3339Nano), Snapshot: "live", Exact: true,
			From: plan.Context.From.Format(time.RFC3339Nano), To: plan.Context.To.Format(time.RFC3339Nano), Filters: append([]signalquery.Filter{}, plan.Request.Filters...),
			SeriesComplete: true, GroupsTruncated: false, GroupBy: append([]signalquery.Dimension{}, plan.Request.GroupBy...), PointBudget: plan.Request.Points,
		}, Summary: nil, Groups: make([]Row, 0, plan.Request.Groups), Rows: []Row{},
	}
	keys := make([]string, 0, plan.Request.Groups)
	seriesIDs := make(map[string]string, plan.Request.Groups)
	if plan.Request.Trigger == "stream" {
		result.Metadata.RunID = ""
	}
	for _, row := range top {
		if row["is_total"] == int64(1) {
			delete(row, "is_total")
			delete(row, "group_key")
			if err := normalizeSignalMeasures(row); err != nil {
				return nil, err
			}
			result.Summary = row
			continue
		}
		if len(keys) == plan.Request.Groups {
			result.Metadata.GroupsTruncated = true
			continue
		}
		key, ok := row["group_key"].(string)
		if !ok {
			return nil, fmt.Errorf("signal group has no identity")
		}
		keys = append(keys, key)
		if err := normalizeSignalRow(row); err != nil {
			return nil, err
		}
		seriesID, ok := row["series_id"].(string)
		if !ok {
			return nil, fmt.Errorf("signal series has no display identity")
		}
		seriesIDs[key] = seriesID
		result.Groups = append(result.Groups, row)
	}
	if len(keys) == 0 {
		return result, nil
	}
	series, err := plan.Series(keys)
	if err != nil {
		return nil, signalFailure(err)
	}
	rows, err := runSignalPlan(ctx, e.ch, series)
	if err != nil {
		return nil, err
	}
	if len(rows) > plan.Request.Points {
		return nil, newError(ErrLimitExceeded, signalquery.Dataset, "points", "", "complete series exceeds the point budget")
	}
	byBucket := make(map[string]Row, len(rows))
	for _, row := range rows {
		key, keyOK := row["group_key"].(string)
		at, timeOK := row["time_bucket"].(string)
		if !keyOK || !timeOK {
			return nil, fmt.Errorf("signal point has no bucket identity")
		}
		byBucket[at+"|"+key] = row
	}
	for at := signalquery.BucketStart(plan.Context.From, plan.Context.Step); at.Before(plan.Context.To); at = signalquery.NextBucket(at, plan.Context.Step) {
		next := signalquery.NextBucket(at, plan.Context.Step)
		stamp := at.Format(time.RFC3339Nano)
		for _, key := range keys {
			row, present := byBucket[stamp+"|"+key]
			if !present {
				row = Row{"group_key": key, "time_bucket": stamp, "observed_outcomes": int64(0), "answered": int64(0), "unmeasured": int64(0), "failed": int64(0), "not_applicable": int64(0), "draft": int64(0), "evaluated_sessions": int64(0), "matched_messages": int64(0), "matched_sessions": int64(0), "definition_count": int64(0)}
			}
			row["bucket_end"] = next.Format(time.RFC3339Nano)
			row["partial"] = at.Before(plan.Context.From) || next.After(plan.Context.To)
			row["observed"] = present
			row["series_id"] = seriesIDs[key]
			delete(row, "group_key")
			if err := normalizeSignalMeasures(row); err != nil {
				return nil, err
			}
			result.Rows = append(result.Rows, row)
		}
	}
	return result, nil
}

// signalManualContext pins a whole run to its one immutable request timestamp
// before any canonical read can prune request-time partitions.
func (e *Engine) signalManualContext(ctx context.Context, scope signalquery.Context, runID string) (signalquery.Context, error) {
	cohort, err := runSignalPlan(ctx, e.ch, signalquery.Query{
		SQL: `SELECT uniqExact(evaluation_requested_at) AS request_times, min(evaluation_requested_at) AS requested_at
 FROM sigint_readings_v2 WHERE organization_id = ? AND project_id = toUUID(?) AND trigger_kind = 'manual' AND run_id = toUUID(?)
 LIMIT 1 SETTINGS max_execution_time = 5, timeout_before_checking_execution_speed = 0, max_rows_to_read = 25000000, max_bytes_to_read = 4000000000, max_memory_usage = 1000000000`,
		Args: []any{scope.OrganizationID, scope.ProjectID, runID},
	})
	if err != nil {
		return scope, err
	}
	if len(cohort) != 1 {
		return scope, fmt.Errorf("manual run has no request timestamp summary")
	}
	if cohort[0]["request_times"] == int64(0) {
		// An absent run has an empty, expired canonical population.
		scope.RequestedAt = new(time.Unix(0, 0).UTC())
		return scope, nil
	}
	if cohort[0]["request_times"] != int64(1) {
		return scope, newError(ErrUnsatisfiable, signalquery.Dataset, "run_id", "", "the manual run contains inconsistent request timestamps")
	}
	stamp, ok := cohort[0]["requested_at"].(string)
	if !ok {
		return scope, fmt.Errorf("manual run has no request timestamp")
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return scope, fmt.Errorf("parse manual request timestamp: %w", err)
	}
	scope.RequestedAt = &at
	return scope, nil
}

func runSignalPlan(ctx context.Context, conn Querier, query signalquery.Query) ([]Row, error) {
	plan := Plan{Dataset: signalquery.Dataset, Name: "signals.canonical_final", SQL: query.SQL, Args: query.Args, Columns: nil}
	rows, err := plan.Run(ctx, conn)
	if err != nil {
		return nil, signalFailure(err)
	}
	return rows, nil
}

func signalFailure(err error) error {
	if invalid, ok := errors.AsType[*signalquery.Error](err); ok {
		if invalid.LimitExceeded {
			return newError(ErrLimitExceeded, signalquery.Dataset, invalid.Field, "", invalid.Message)
		}
		return newError(ErrUnsatisfiable, signalquery.Dataset, invalid.Field, "", invalid.Message)
	}
	if exception, ok := errors.AsType[*clickhouse.Exception](err); ok {
		switch exception.Code {
		case 158, 159, 241, 307, 396:
			return newError(ErrLimitExceeded, signalquery.Dataset, "window", "", "this range exceeds the interactive read budget; choose a shorter time range")
		}
	}
	return err
}

func normalizeSignalRow(row Row) error {
	key, ok := row["group_key"].(string)
	if !ok {
		return fmt.Errorf("signal group key is not a string")
	}
	var pairs [][]any
	if err := json.Unmarshal([]byte(key), &pairs); err != nil {
		return fmt.Errorf("decode signal group: %w", err)
	}
	values := make([]any, len(pairs))
	for i, pair := range pairs {
		if len(pair) != 2 {
			return fmt.Errorf("signal group has invalid label tuple")
		}
		if pair[0] == float64(1) || pair[0] == true {
			values[i] = pair[1]
		}
	}
	hash := sha256.Sum256([]byte(key))
	row["series_id"], row["group_values"] = hex.EncodeToString(hash[:]), values
	delete(row, "group_key")
	delete(row, "is_total")
	return normalizeSignalMeasures(row)
}

func normalizeSignalMeasures(row Row) error {
	answered, _ := row["answered"].(int64)
	matched, _ := row["matched_messages"].(int64)
	evaluatedSessions, _ := row["evaluated_sessions"].(int64)
	matchedSessions, _ := row["matched_sessions"].(int64)
	row["match_share"], row["session_breadth"] = nil, nil
	if row["observed_outcomes"] == int64(0) {
		row["latest_inserted_at"] = nil
	}
	if answered > 0 {
		row["match_share"] = float64(matched) / float64(answered)
	} else {
		row["matched_messages"] = nil
		row["matched_sessions"] = nil
	}
	if evaluatedSessions > 0 {
		row["session_breadth"] = float64(matchedSessions) / float64(evaluatedSessions)
	}
	if raw, ok := row["probability_bins"]; ok {
		data, err := json.Marshal(raw)
		if err != nil {
			return fmt.Errorf("encode probability distribution: %w", err)
		}
		var sparse [2][]uint64
		if err := json.Unmarshal(data, &sparse); err != nil {
			return fmt.Errorf("decode probability distribution: %w", err)
		}
		bins := make([]uint64, 11)
		if len(sparse[0]) != len(sparse[1]) {
			return fmt.Errorf("probability distribution is incomplete")
		}
		for i, key := range sparse[0] {
			if key > 10 {
				return fmt.Errorf("probability bin exceeds unit interval")
			}
			bins[key] = sparse[1][i]
		}
		row["probability_bins"] = bins
		if answered == 0 {
			row["probability_bins"] = nil
		}
	}
	return nil
}
