package signals

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

var sql = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// Query limits are per physical read. Overflow throws rather than returning
// incomplete charts. FINAL partition independence follows immutable request time.
const settings = ` SETTINGS do_not_merge_across_partitions_select_final = 1,
 optimize_move_to_prewhere_if_final = 0, max_execution_time = 5,
 timeout_before_checking_execution_speed = 0, max_rows_to_read = 25000000,
 max_bytes_to_read = 4000000000, max_memory_usage = 1000000000,
 max_result_rows = 10002, max_result_bytes = 16000000, result_overflow_mode = 'throw'`

// Compile validates and compiles exact, key-qualified canonical reads.
func Compile(ctx Context, request Request) (*Plan, error) {
	if err := validateScope(&ctx, &request); err != nil {
		return nil, err
	}
	if err := validateSelection(request.Selection); err != nil {
		return nil, err
	}
	source := canonicalSource(ctx, request)
	prepared := sql.Select("*").FromSelect(source, "winners")
	for _, filter := range request.Filters {
		expression, args := dimension(filter.Dimension)
		switch filter.Operator {
		case "exists":
			prepared = prepared.Where("tupleElement("+expression+", 1)", args...)
		case "missing":
			prepared = prepared.Where("NOT tupleElement("+expression+", 1)", args...)
		default:
			marks := strings.TrimSuffix(strings.Repeat("?,", len(filter.Values)), ",")
			arguments := slices.Clone(args)
			arguments = append(arguments, args...)
			for _, value := range filter.Values {
				arguments = append(arguments, value)
			}
			prepared = prepared.Where("tupleElement("+expression+", 1) AND tupleElement("+expression+", 2) IN ("+marks+")", arguments...)
		}
	}
	groupExpressions := make([]string, 0, len(request.GroupBy))
	var groupArgs []any
	for _, group := range request.GroupBy {
		expression, args := dimension(group)
		groupExpressions = append(groupExpressions, expression)
		groupArgs = append(groupArgs, args...)
	}
	groupExpression := "'[]'"
	if len(groupExpressions) > 0 {
		groupExpression = "toJSONString([" + strings.Join(groupExpressions, ",") + "])"
	}
	prepared = prepared.Column(groupExpression+" AS group_key", groupArgs...)
	probability, population, args := predicate(request.Selection)
	var populationArgs []any
	if request.Selection.MinimumLevel == nil {
		populationArgs = []any{request.Selection.SignalID}
	}
	prepared = prepared.Column("ifNull("+probability+", 0) AS match_probability", args...).
		Column("(status = 'answered' AND ("+population+")) AS measured", populationArgs...).
		Column("(measured AND match_probability >= ?) AS hit", request.Selection.Threshold)
	preparedQuery, err := build(prepared, false)
	if err != nil {
		return nil, err
	}
	groups := sql.Select("group_key", "grouping(group_key) AS is_total").
		Columns(measures...).FromSelect(prepared, "observations").
		GroupBy("GROUPING SETS ((group_key), ())").
		OrderBy("is_total DESC", "matched_messages DESC", "answered DESC", "group_key ASC").
		Suffix(fmt.Sprintf("LIMIT %d", request.Groups+2))
	groupQuery, err := build(groups, true)
	if err != nil {
		return nil, err
	}
	return &Plan{Context: ctx, Request: request, Groups: groupQuery, Prepared: preparedQuery}, nil
}

// measures are semantic companions computed from the same resolved population.
// The denominator excludes answered definitions that did not measure the chosen
// signal/level. Distinct conversations are exact at each requested grouping.
var measures = []string{
	"count() AS observed_outcomes",
	"countIf(measured) AS answered",
	"countIf(status = 'answered' AND NOT measured) AS unmeasured",
	"countIf(hit) AS matched_messages",
	"countIf(status = 'failed') AS failed",
	"countIf(status = 'not_applicable') AS not_applicable",
	"countIf(status = 'draft') AS draft",
	"uniqExactIf(conversation_id, measured AND isNotNull(conversation_id)) AS evaluated_sessions",
	"uniqExactIf(conversation_id, hit AND isNotNull(conversation_id)) AS matched_sessions",
	"uniqExactIf(definition_hash, definition_hash != '') AS definition_count",
	"groupUniqArrayIf(16)(definition_hash, definition_hash != '') AS definition_hashes",
	"groupUniqArray(16)(configured_model) AS classifier_models",
	"groupUniqArray(16)(compiler_version) AS compiler_versions",
	"groupUniqArray(16)(input_strategy) AS input_strategies",
	"max(inserted_at) AS latest_inserted_at",
	"sumMap([toUInt64(least(floor(match_probability * 10), 10))], [toUInt64(measured)]) AS probability_bins",
}

func predicate(selection Selection) (probability, population string, args []any) {
	if selection.MinimumLevel != nil {
		// The cutoff is validated as a small integer before entering SQL.
		cutoff := *selection.MinimumLevel
		return fmt.Sprintf("if(result_type = 'score', arraySum(arraySlice(signal_probabilities, %d)), 0)", cutoff+1),
			fmt.Sprintf("result_type = 'score' AND length(signal_probabilities) > %d", cutoff), nil
	}
	return "if(result_type = 'choice', toFloat64(selected_signal_id = toUUID(?)), if(result_type = 'multi_label', signal_probabilities[indexOf(signal_ids, toUUID(?))], 0))",
		"result_type IN ('choice', 'multi_label') AND has(signal_ids, toUUID(?))",
		[]any{selection.SignalID, selection.SignalID}
}

// Series compiles complete time series for groups selected across the full window.
func (p *Plan) Series(keys []string) (Query, error) {
	if len(keys) == 0 || len(keys) > p.Request.Groups {
		return Query{}, &Error{Field: "groups", Message: "select at least one group within the group budget"}
	}
	step := bucket(p.Context.Step)
	q := sql.Select(step+" AS time_bucket", "group_key").Columns(measures...).
		From("("+p.Prepared.SQL+") AS observations").
		Where(squirrel.Eq{"group_key": keys}).
		GroupBy("time_bucket", "group_key").OrderBy("time_bucket", "group_key").Suffix(fmt.Sprintf("LIMIT %d", p.Request.Points+1))
	query, err := build(q, true)
	if err != nil {
		return Query{}, err
	}
	query.Args = append(slices.Clone(p.Prepared.Args), query.Args...)
	return query, nil
}

func build(builder squirrel.SelectBuilder, bounded bool) (Query, error) {
	query, args, err := builder.ToSql()
	if err != nil {
		return Query{}, fmt.Errorf("compile signal measurement: %w", err)
	}
	if bounded {
		query += settings
	}
	return Query{SQL: query, Args: args}, nil
}

func validateScope(ctx *Context, request *Request) error {
	invalid := func(field, message string) error { return &Error{Field: field, Message: message} }
	if _, err := uuid.Parse(ctx.ProjectID); err != nil || ctx.OrganizationID == "" {
		return invalid("project", "an authorized organization and project are required")
	}
	if ctx.Now.IsZero() || !ctx.To.After(ctx.From) || ctx.To.Sub(ctx.From) > MaxSpanDays*24*time.Hour || ctx.From.Year() < 1970 || ctx.To.Year() > 2100 {
		return invalid("window", "choose a half-open source-time window of at most ten years between 1970 and 2100")
	}
	if _, err := uuid.Parse(request.Selection.SensorID); err != nil || request.Selection.SensorID == uuid.Nil.String() {
		return invalid("selection.sensor_id", "name one sensor by its UUID")
	}
	if request.Trigger == "" {
		request.Trigger = "stream"
	}
	switch request.Trigger {
	case "stream":
		if request.RunID != "" && request.RunID != uuid.Nil.String() {
			return invalid("run_id", "streaming has no manual run")
		}
		request.RunID = uuid.Nil.String()
	case "manual":
		id, err := uuid.Parse(request.RunID)
		if err != nil || id == uuid.Nil {
			return invalid("run_id", "manual evaluation requires one explicit run UUID")
		}
		request.RunID = id.String()
	default:
		return invalid("trigger", "choose stream or manual")
	}
	if len(request.GroupBy) > 3 || len(request.Filters) > MaxFilters {
		return invalid("dimensions", "use at most three groupings and twenty filters")
	}
	seen := map[Dimension]bool{}
	selectorBytes := 0
	for _, group := range request.GroupBy {
		selectorBytes += len(group.Attribute)
		if !validDimension(group) || seen[group] {
			return invalid("group_by", "use unique system dimensions or nonempty attribute keys of at most 256 bytes")
		}
		seen[group] = true
	}
	for _, filter := range request.Filters {
		selectorBytes += len(filter.Dimension.Attribute)
		for _, value := range filter.Values {
			selectorBytes += len(value)
		}
		if !validDimension(filter.Dimension) {
			return invalid("filters", "unknown dimension")
		}
		switch filter.Operator {
		case "exists", "missing":
			if len(filter.Values) != 0 {
				return invalid("filters.values", "presence filters take no values")
			}
		case "equals", "in":
			if len(filter.Values) == 0 || len(filter.Values) > MaxValues || filter.Operator == "equals" && len(filter.Values) != 1 {
				return invalid("filters.values", "equals takes one value; in takes 1–100")
			}
		default:
			return invalid("filters.operator", "choose equals, in, exists or missing")
		}
	}
	if selectorBytes > MaxSelectorBytes {
		return invalid("filters", "attribute selectors exceed the 64 KiB query budget")
	}
	if request.Groups == 0 {
		request.Groups = DefaultGroups
	}
	if request.Points == 0 {
		request.Points = DefaultPoints
	}
	if request.Groups < 1 || request.Groups > MaxGroups || request.Points < 1 || request.Points > MaxPoints {
		return invalid("budget", "use 1–20 groups and 1–10000 total points")
	}
	groups := request.Groups
	if len(request.GroupBy) == 0 {
		groups = 1
	}
	if ctx.Step == "" || ctx.Step == "auto" {
		for _, step := range []string{"minute", "hour", "day", "week", "month"} {
			if BucketCount(ctx.From, ctx.To, step)*groups <= request.Points {
				ctx.Step = step
				break
			}
		}
	}
	if bucket(ctx.Step) == "" {
		return invalid("grain", "choose auto, minute, hour, day, week or calendar month")
	}
	if BucketCount(ctx.From, ctx.To, ctx.Step)*groups > request.Points {
		return invalid("points", "the complete series exceeds the point budget; use auto, fewer groups or a shorter window")
	}
	ctx.From, ctx.To, ctx.Now = ctx.From.UTC(), ctx.To.UTC(), ctx.Now.UTC()
	return nil
}

func validateSelection(selection Selection) error {
	if selection.MinimumLevel != nil {
		if selection.SignalID != "" || *selection.MinimumLevel < 0 || *selection.MinimumLevel > 9 {
			return &Error{Field: "selection.minimum_level", Message: "choose one zero-based ordered level (0–9), without signal_id"}
		}
	} else if id, err := uuid.Parse(selection.SignalID); err != nil || id == uuid.Nil {
		return &Error{Field: "selection.signal_id", Message: "name a signal or choose an ordered minimum_level"}
	}
	if math.IsNaN(selection.Threshold) || math.IsInf(selection.Threshold, 0) || selection.Threshold <= 0 || selection.Threshold > 1 {
		return &Error{Field: "selection.threshold", Message: "choose an explicit probability threshold greater than zero and at most one"}
	}
	return nil
}

func canonicalSource(ctx Context, request Request) squirrel.SelectBuilder {
	source := sql.Select("id", "event_id", "event_occurred_at", "conversation_id", "conversation_role", "source", "actor_user_id", "assistant_id", "attributes", "status", "definition_hash", "configured_model", "compiler_version", "input_strategy", "inserted_at", "result_type", "selected_signal_id", "probabilities.signal_id AS signal_ids", "probabilities.probability AS signal_probabilities").
		From("sigint_readings_v2 FINAL").
		Where("organization_id = ? AND project_id = toUUID(?)", ctx.OrganizationID, ctx.ProjectID).
		Where("trigger_kind = ? AND run_id = toUUID(?) AND sensor_id = toUUID(?)", request.Trigger, request.RunID, request.Selection.SensorID).
		Where("event_occurred_at >= fromUnixTimestamp64Nano(?) AND event_occurred_at < fromUnixTimestamp64Nano(?)", ctx.From.UnixNano(), ctx.To.UnixNano()).
		Where("addYears(toDateTime(evaluation_requested_at, 'UTC'), 2) > fromUnixTimestamp64Nano(?)", ctx.Now.UnixNano()).
		Where("event_kind = 'conversation.message'").
		Where("conversation_capture_kind = 'original'")
	if request.Trigger == "manual" && ctx.RequestedAt != nil {
		source = source.Where("evaluation_requested_at = fromUnixTimestamp64Nano(?)", ctx.RequestedAt.UnixNano())
	}
	return source
}

// CompileValues discovers keys or values from winning snapshots, including absence.
func CompileValues(ctx Context, scope Scope, name, attribute string, limit int) (Query, error) {
	request := Request{Selection: Selection{SensorID: scope.SensorID, SignalID: "", MinimumLevel: nil, Threshold: 0}, Trigger: scope.Trigger, RunID: scope.RunID, GroupBy: nil, Filters: nil, Groups: 1, Points: MaxPoints}
	ctx.Step = "auto"
	if err := validateScope(&ctx, &request); err != nil {
		return Query{}, err
	}
	if limit < 1 || limit > 200 {
		return Query{}, &Error{Field: "limit", Message: "choose 1–200 values"}
	}
	q := sql.Select().FromSelect(canonicalSource(ctx, request), "winners")
	if name == "attribute_keys" && attribute == "" {
		q = q.Column("arrayJoin(mapKeys(attributes)) AS value").Column("toUInt8(1) AS present")
	} else {
		d := Dimension{Field: name, Attribute: ""}
		if name == "attribute" {
			d = Dimension{Field: "", Attribute: attribute}
		} else if attribute != "" {
			return Query{}, &Error{Field: "attribute", Message: "attribute keys require dimension=attribute"}
		}
		if !validDimension(d) {
			return Query{}, &Error{Field: "dimension", Message: "choose a system dimension, attribute_keys, or attribute with an exact key"}
		}
		expr, args := dimension(d)
		q = q.Column("tupleElement("+expr+", 2) AS value", args...).Column("tupleElement("+expr+", 1) AS present", args...)
	}
	return build(q.Column("count() AS n").GroupBy("value", "present").OrderBy("n DESC", "present DESC", "value ASC").Suffix(fmt.Sprintf("LIMIT %d", limit)), true)
}

var systemDimensions = map[string]string{"source": "source", "role": "conversation_role", "actor": "actor_user_id", "assistant": "toString(assistant_id)", "definition": "definition_hash"}

func validDimension(d Dimension) bool {
	if d.Attribute != "" {
		return d.Field == "" && len(d.Attribute) <= 256
	}
	_, ok := systemDimensions[d.Field]
	return ok
}

func dimension(d Dimension) (string, []any) {
	if d.Attribute != "" {
		return "tuple(mapContains(attributes, ?), attributes[?])", []any{d.Attribute, d.Attribute}
	}
	expression := "ifNull(" + systemDimensions[d.Field] + ", '')"
	return "tuple(notEmpty(" + expression + "), " + expression + ")", nil
}

func bucket(step string) string {
	switch step {
	case "minute":
		return "toStartOfMinute(event_occurred_at)"
	case "hour":
		return "toStartOfHour(event_occurred_at)"
	case "day":
		return "toStartOfDay(event_occurred_at, 'UTC')"
	case "week":
		return "toStartOfWeek(event_occurred_at, 1, 'UTC')"
	case "month":
		return "toStartOfMonth(event_occurred_at)"
	default:
		return ""
	}
}

// BucketStart uses UTC calendar boundaries, including real calendar months.
func BucketStart(t time.Time, step string) time.Time {
	t = t.UTC()
	switch step {
	case "minute":
		return t.Truncate(time.Minute)
	case "hour":
		return t.Truncate(time.Hour)
	case "day", "week":
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		if step == "week" {
			return d.AddDate(0, 0, -(int(d.Weekday())+6)%7)
		}
		return d
	case "month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return t
	}
}

// NextBucket advances one calendar interval without approximating month length.
func NextBucket(t time.Time, step string) time.Time {
	switch step {
	case "minute":
		return t.Add(time.Minute)
	case "hour":
		return t.Add(time.Hour)
	case "day":
		return t.AddDate(0, 0, 1)
	case "week":
		return t.AddDate(0, 0, 7)
	case "month":
		return t.AddDate(0, 1, 0)
	default:
		return t.AddDate(1, 0, 0)
	}
}

// BucketCount stops once no legal request can fit; it never walks ten years of minutes.
func BucketCount(from, to time.Time, step string) int {
	count := 0
	for at := BucketStart(from, step); at.Before(to) && count <= MaxPoints; at = NextBucket(at, step) {
		count++
	}
	return count
}
