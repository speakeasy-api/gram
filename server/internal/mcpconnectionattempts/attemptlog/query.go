package attemptlog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// Retention is how long attempt rows are kept, so how far back a read may start.
	Retention = 30 * 24 * time.Hour

	// FoldGap is how soon after one failure ends the next must start to fold into
	// the same row. Retry loops repeat within minutes; a longer gap is a new episode.
	FoldGap = 15 * time.Minute

	// DefaultPageSize and MaxPageSize bound one page of the list.
	DefaultPageSize = 50
	MaxPageSize     = 100

	// maxCubeRows bounds the counts query. Each row is one distinct combination of
	// filter values; a project rarely has more than a few thousand in 30 days.
	maxCubeRows = 20_000

	// MaxFacetValues bounds the values returned per filter.
	MaxFacetValues = 100

	// maxDeniedClients bounds the denied clients list.
	maxDeniedClients = 50

	// maxAttemptSteps bounds the steps read for one attempt. Real attempts have fewer than 20.
	maxAttemptSteps = 500

	// rangeSlack lets a "last 30 days" range computed a moment earlier by the caller still pass.
	rangeSlack = 5 * time.Minute
)

// readSettings bound one read: past the spill threshold GROUP BY and sorts go to
// disk, and a read over the cap fails alone instead of starving the server.
const readSettings = ` SETTINGS max_memory_usage = 1610612736,
 max_bytes_before_external_group_by = 536870912,
 max_bytes_before_external_sort = 536870912,
 max_execution_time = 30`

// ErrBadCursor is a cursor this package did not issue.
var ErrBadCursor = errors.New("invalid cursor")

// Scope is the attempts a read may see. Organization, project and the start range are required.
type Scope struct {
	// OrganizationID owns the attempts.
	OrganizationID string

	// ProjectID owns the attempts.
	ProjectID uuid.UUID

	// IssuerID keeps the provider's attempts. Nil keeps every attempt in the project.
	IssuerID uuid.UUID

	// IssuerServerIDs are the servers and gateways attached to IssuerID now. Attempts on
	// them whose rows name no issuer belong to it.
	IssuerServerIDs []string

	// ServerID keeps one server, gateway or toolset when set.
	ServerID string

	// AttemptID keeps one attempt when set.
	AttemptID uuid.UUID

	// From bounds attempt start, inclusive.
	From time.Time

	// To bounds attempt start, exclusive.
	To time.Time

	// OwnSubjects, when not nil, keeps only attempts by these subjects.
	OwnSubjects []string
}

// Filters narrow attempts. Values within a field are alternatives; fields combine.
type Filters struct {
	Outcomes    []string
	Steps       []string
	ServerIDs   []string
	MemberIDs   []string
	People      []string
	Networks    []string
	Apps        []string
	AppVersions []string
	Reasons     []string
	Services    []string
}

// Row is one row of the list: one attempt, or a run of the same failure.
type Row struct {
	// ID is the newest attempt's id, its reference.
	ID uuid.UUID `ch:"r_id"`

	// Time is when the newest attempt ended or last moved.
	Time time.Time `ch:"r_time"`

	// FirstStartedAt is the earliest start in the row.
	FirstStartedAt time.Time `ch:"r_first"`

	// Count is the attempts the row stands for, repeats included.
	Count uint64 `ch:"r_count"`

	// Outcome is an Outcome.
	Outcome string `ch:"r_outcome"`

	// Step is a StepGroup.
	Step string `ch:"r_step"`

	// StoredStep is the step that ended the newest attempt.
	StoredStep string `ch:"r_stored_step"`

	// Reason is the reason code, empty for success.
	Reason string `ch:"r_reason"`

	// Subject is the person's subject URN, empty for none.
	Subject string `ch:"r_subject"`

	// Network is the network key, empty for none.
	Network string `ch:"r_network"`

	// ServerID is the server, gateway, toolset or issuer, empty for none.
	ServerID string `ch:"r_server"`

	// ServerKind is the stored kind of server.
	ServerKind string `ch:"r_kind"`

	// App is the app family, empty when not on the list.
	App string `ch:"r_app"`

	// AppKey is the stored app key.
	AppKey string `ch:"r_app_key"`

	// AppVersion is the reported version, empty for none.
	AppVersion string `ch:"r_version"`

	// MemberID is the gateway member on the ending step.
	MemberID string `ch:"r_member"`

	// Service is the service host.
	Service string `ch:"r_service"`

	// TraceID is the trace of the ending step.
	TraceID string `ch:"r_trace"`

	// GrantType is the grant on the ending step.
	GrantType string `ch:"r_grant"`

	// StartedAt is the newest attempt's start.
	StartedAt time.Time `ch:"r_started"`

	// IssuerIDs are the issuers the newest attempt's rows name.
	IssuerIDs []string `ch:"r_issuers"`
}

// Cursor is where the next page starts.
type Cursor struct {
	// Time is the last row's time.
	Time time.Time

	// ID is the last row's id.
	ID uuid.UUID
}

type cursorJSON struct {
	T int64     `json:"t"`
	I uuid.UUID `json:"i"`
}

// Encode returns the opaque form.
func (c Cursor) Encode() string {
	raw, _ := json.Marshal(cursorJSON{T: c.Time.UnixNano(), I: c.ID}) //nolint:errchkjson // a fixed struct always marshals
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor parses an opaque cursor.
func DecodeCursor(value string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, ErrBadCursor
	}
	var parsed cursorJSON
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.I == uuid.Nil {
		return Cursor{}, ErrBadCursor
	}
	return Cursor{Time: time.Unix(0, parsed.T).UTC(), ID: parsed.I}, nil
}

// ValidateRange checks a start range against retention.
func ValidateRange(from, to, now time.Time) error {
	switch {
	case !from.Before(to):
		return errors.New("from must be before to")
	case from.Before(now.Add(-Retention - rangeSlack)):
		return errors.New("from must be within the last 30 days")
	case to.Sub(from) > Retention:
		return errors.New("the range must be at most 30 days")
	default:
		return nil
	}
}

// attemptsSQL reduces each attempt in scope to one row of derived columns. The
// ending tuple follows the same order and tie-breaks as mcpconnectionattempts.Summarize.
func attemptsSQL(s Scope, now time.Time) (string, []any, error) {
	if s.OrganizationID == "" || s.ProjectID == uuid.Nil || s.From.IsZero() || s.To.IsZero() {
		return "", nil, errors.New("attempt scope needs organization, project and range")
	}
	from, to := s.From.UnixNano(), s.To.UnixNano()
	base := []string{
		"organization_id = ?",
		"project_id = ?",
		"attempt_started_at >= fromUnixTimestamp64Nano(?)",
		"attempt_started_at < fromUnixTimestamp64Nano(?)",
	}
	baseArgs := []any{s.OrganizationID, s.ProjectID, from, to}

	conds := slices.Clone(base)
	args := slices.Clone(baseArgs)
	if s.ServerID != "" {
		conds, args = append(conds, "server_id = ?"), append(args, s.ServerID)
	}
	if s.AttemptID != uuid.Nil {
		conds, args = append(conds, "attempt_id = ?"), append(args, s.AttemptID)
	}
	if s.OwnSubjects != nil && s.AttemptID == uuid.Nil {
		conds = append(conds, `attempt_id IN (
			SELECT attempt_id FROM `+mcpconnectionattempts.EventsTable+` WHERE `+strings.Join(base, " AND ")+` AND has(?, subject_urn))`)
		args = append(args, baseArgs...)
		args = append(args, nonNil(s.OwnSubjects))
	}
	if s.IssuerID != uuid.Nil {
		conds = append(conds, `(has(?, server_id) OR server_id = ? OR attempt_id IN (
			SELECT attempt_id FROM `+mcpconnectionattempts.EventsTable+` WHERE `+strings.Join(base, " AND ")+` AND user_session_issuer_id = ?))`)
		args = append(args, nonNil(s.IssuerServerIDs), s.IssuerID.String())
		args = append(args, baseArgs...)
		args = append(args, s.IssuerID.String())
	}

	outer := []string{"1"}
	if s.IssuerID != uuid.Nil {
		outer, args = append(outer, "(length(a_issuers) = 0 OR has(a_issuers, ?))"), append(args, s.IssuerID.String())
	}
	if s.OwnSubjects != nil {
		outer, args = append(outer, "has(?, a_subject)"), append(args, nonNil(s.OwnSubjects))
	}

	liveCutoff := now.Add(-mcpconnectionattempts.AttemptLiveWindow).UnixNano()
	args = append([]any{
		listedFamilies(),
		liveCutoff,
		reasonsWithOutcome(mcpconnectionattempts.OutcomeBlocked),
		reasonsWithOutcome(mcpconnectionattempts.OutcomeDeclined),
	}, args...)

	query := `
	SELECT
		a_id, a_started, a_weight, a_subject, a_device, a_server, a_kind, a_app_key, a_issuers,
		if(has(?, a_family), a_family, '') AS d_app,
		a_version AS d_version,
		if(a_version != '', concat(d_app, '@', a_version), '') AS d_app_version,
		tupleElement(a_end, 1) AS d_stored_step,
		tupleElement(a_end, 3) AS d_reason,
		tupleElement(a_end, 4) AS d_grant,
		tupleElement(a_end, 5) AS d_member,
		tupleElement(a_end, 6) AS d_trace,
		if(tupleElement(a_end, 7) != '', tupleElement(a_end, 7), a_first_service) AS d_service,
		tupleElement(a_end, 8) AS d_at,
		toString(multiIf(
			a_has_final, tupleElement(a_end, 2),
			a_started > fromUnixTimestamp64Nano(?), '` + string(OutcomeInProgress) + `',
			tupleElement(a_end, 3) = '', '` + string(OutcomeDidntFinish) + `',
			has(?, tupleElement(a_end, 3)), '` + string(OutcomeBlocked) + `',
			has(?, tupleElement(a_end, 3)), '` + string(OutcomeDeclined) + `',
			'` + string(OutcomeFailed) + `')) AS d_outcome,
		toString(` + groupSQL("d_stored_step", "d_grant") + `) AS d_step
	FROM (
		SELECT
			attempt_id AS a_id,
			min(attempt_started_at) AS a_started,
			toUInt64(max(repeats)) + 1 AS a_weight,
			argMinIf(subject_urn, (event_time, subject_urn), subject_urn != '') AS a_subject,
			argMinIf(device_key, (event_time, device_key), device_key != '') AS a_device,
			any(server_id) AS a_server,
			toString(any(server_kind)) AS a_kind,
			any(app_key) AS a_app_key,
			groupUniqArrayIf(user_session_issuer_id, user_session_issuer_id != '') AS a_issuers,
			toString(if(countIf(client_family NOT IN ('', 'browser')) > 0,
				argMinIf(client_family, (event_time, client_family), client_family NOT IN ('', 'browser')),
				if(countIf(client_family = 'browser') > 0, 'browser', ''))) AS a_family,
			argMinIf(client_version, (event_time, client_version), client_version != '') AS a_version,
			argMinIf(provider_host, (event_time, provider_host), provider_host != '') AS a_first_service,
			countIf(is_final) > 0 AS a_has_final,
			if(a_has_final,
				argMinIf((toString(step), toString(outcome), toString(reason), toString(grant_type), member_id, trace_id, provider_host, event_time),
					(event_time, step, outcome, reason), is_final),
				argMax((toString(step), toString(outcome), toString(reason), toString(grant_type), member_id, trace_id, provider_host, event_time),
					(event_time, step, reason))) AS a_end
		FROM ` + mcpconnectionattempts.EventsTable + `
		WHERE ` + strings.Join(conds, " AND ") + `
		GROUP BY attempt_id
	)
	WHERE ` + strings.Join(outer, " AND ")
	return query, args, nil
}

func filterSQL(f Filters) (string, []any) {
	conds := []string{"1"}
	var args []any
	add := func(column string, values []string) {
		if len(values) > 0 {
			conds, args = append(conds, "has(?, "+column+")"), append(args, values)
		}
	}
	add("d_outcome", f.Outcomes)
	add("d_step", f.Steps)
	add("a_server", f.ServerIDs)
	add("d_member", f.MemberIDs)
	add("a_subject", f.People)
	// A network stands for attempts with no known person, as in its counts.
	if len(f.Networks) > 0 {
		conds, args = append(conds, "(a_subject = '' AND a_device != '' AND has(?, a_device))"), append(args, f.Networks)
	}
	add("d_app", f.Apps)
	add("d_app_version", f.AppVersions)
	add("d_reason", f.Reasons)
	add("d_service", f.Services)
	return strings.Join(conds, " AND "), args
}

// List reads one page of rows, newest first. A nil cursor is the first page.
func List(ctx context.Context, ch driver.Conn, s Scope, f Filters, after *Cursor, limit int, now time.Time) ([]Row, *Cursor, error) {
	if limit < 1 || limit > MaxPageSize {
		return nil, nil, fmt.Errorf("page size must be between 1 and %d", MaxPageSize)
	}
	attempts, args, err := attemptsSQL(s, now)
	if err != nil {
		return nil, nil, err
	}
	where, filterArgs := filterSQL(f)
	args = append(args, filterArgs...)
	args = append(args, int64(FoldGap/time.Second))

	page := "1"
	if after != nil {
		page = "(r_time, r_id) < (fromUnixTimestamp64Nano(?), toUUID(?))"
		args = append(args, after.Time.UnixNano(), after.ID.String())
	}
	args = append(args, limit+1)

	failing := "('" + string(OutcomeFailed) + "', '" + string(OutcomeBlocked) + "')"
	query := `
	WITH attempts AS (SELECT * FROM (` + attempts + `) WHERE ` + where + `)
	SELECT * FROM (
		SELECT
			argMax(a_id, (d_at, a_id)) AS r_id,
			max(d_at) AS r_time,
			min(a_started) AS r_first,
			sum(a_weight) AS r_count,
			any(d_outcome) AS r_outcome,
			any(d_step) AS r_step,
			argMax(d_stored_step, (d_at, a_id)) AS r_stored_step,
			any(d_reason) AS r_reason,
			any(a_subject) AS r_subject,
			argMax(a_device, (d_at, a_id)) AS r_network,
			any(a_server) AS r_server,
			any(a_kind) AS r_kind,
			any(d_app) AS r_app,
			argMax(a_app_key, (d_at, a_id)) AS r_app_key,
			any(d_version) AS r_version,
			any(d_member) AS r_member,
			any(d_service) AS r_service,
			argMax(d_trace, (d_at, a_id)) AS r_trace,
			argMax(d_grant, (d_at, a_id)) AS r_grant,
			argMax(a_started, (d_at, a_id)) AS r_started,
			argMax(a_issuers, (d_at, a_id)) AS r_issuers
		FROM (
			SELECT *, sum(new_run) OVER (PARTITION BY fold_key ORDER BY a_started, a_id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS run
			FROM (
				SELECT *, if(a_started > lagInFrame(d_at, 1, toDateTime64(0, 9, 'UTC')) OVER (PARTITION BY fold_key ORDER BY a_started, a_id ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) + toIntervalSecond(?), 1, 0) AS new_run
				FROM (
					SELECT *, if(d_outcome IN ` + failing + `,
						cityHash64(multiIf(a_subject != '', a_subject, a_device != '', concat('network:', a_device), concat('attempt:', toString(a_id))), a_server, a_kind, d_member, d_step, d_stored_step, d_outcome, d_reason, d_app, d_version, d_service),
						cityHash64(toString(a_id))) AS fold_key
					FROM attempts
				)
			)
		)
		GROUP BY fold_key, run
	)
	WHERE ` + page + `
	ORDER BY r_time DESC, r_id DESC
	LIMIT ?` + readSettings

	rows, err := scanRows(ctx, ch, query, args)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) <= limit {
		return rows, nil, nil
	}
	rows = rows[:limit]
	last := rows[len(rows)-1]
	return rows, &Cursor{Time: last.Time, ID: last.ID}, nil
}

// Get reads one attempt as a single row and its steps, or nil when the scope does not hold it.
func Get(ctx context.Context, ch driver.Conn, s Scope, now time.Time) (*Row, []Step, error) {
	if s.AttemptID == uuid.Nil {
		return nil, nil, errors.New("get needs an attempt id")
	}
	attempts, args, err := attemptsSQL(s, now)
	if err != nil {
		return nil, nil, err
	}
	query := `
	SELECT
		a_id AS r_id, d_at AS r_time, a_started AS r_first, a_weight AS r_count, d_outcome AS r_outcome,
		d_step AS r_step, d_stored_step AS r_stored_step, d_reason AS r_reason, a_subject AS r_subject,
		a_device AS r_network, a_server AS r_server, a_kind AS r_kind, d_app AS r_app, a_app_key AS r_app_key,
		d_version AS r_version, d_member AS r_member, d_service AS r_service, d_trace AS r_trace,
		d_grant AS r_grant, a_started AS r_started, a_issuers AS r_issuers
	FROM (` + attempts + `)
	LIMIT 1` + readSettings
	rows, err := scanRows(ctx, ch, query, args)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}
	steps, err := readSteps(ctx, ch, s)
	if err != nil {
		return nil, nil, err
	}
	return &rows[0], steps, nil
}

func scanRows(ctx context.Context, ch driver.Conn, query string, args []any) ([]Row, error) {
	result, err := ch.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query connection attempts: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return result.Close() })

	var rows []Row
	for result.Next() {
		var row Row
		if err := result.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan connection attempt: %w", err)
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("read connection attempts: %w", err)
	}
	return rows, nil
}

// Step is one stored step of an attempt.
type Step struct {
	// At is when the step ended.
	At time.Time `ch:"s_at"`

	// StoredStep is the stored step.
	StoredStep string `ch:"s_step"`

	// Result is how the step ended.
	Result string `ch:"s_result"`

	// Final says the step ended the attempt.
	Final bool `ch:"s_final"`

	// Reason is the reason code.
	Reason string `ch:"s_reason"`

	// SubReason is the finer label within the reason.
	SubReason string `ch:"s_sub_reason"`

	// MemberID is the gateway member.
	MemberID string `ch:"s_member"`

	// Service is the service host.
	Service string `ch:"s_service"`

	// GrantType is the OAuth grant.
	GrantType string `ch:"s_grant"`

	// Retries is the retries spent in the step.
	Retries uint32 `ch:"s_retries"`
}

func readSteps(ctx context.Context, ch driver.Conn, s Scope) ([]Step, error) {
	result, err := ch.Query(ctx, `
	SELECT event_time AS s_at, toString(step) AS s_step, toString(step_result) AS s_result, is_final AS s_final,
		toString(reason) AS s_reason, toString(sub_reason) AS s_sub_reason, member_id AS s_member,
		provider_host AS s_service, toString(grant_type) AS s_grant, retries AS s_retries
	FROM `+mcpconnectionattempts.EventsTable+`
	WHERE organization_id = ? AND project_id = ?
		AND attempt_started_at >= fromUnixTimestamp64Nano(?) AND attempt_started_at < fromUnixTimestamp64Nano(?)
		AND attempt_id = ?
	ORDER BY event_time, step
	LIMIT ?`+readSettings, s.OrganizationID, s.ProjectID, s.From.UnixNano(), s.To.UnixNano(), s.AttemptID, maxAttemptSteps)
	if err != nil {
		return nil, fmt.Errorf("query attempt steps: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return result.Close() })

	var steps []Step
	for result.Next() {
		var step Step
		if err := result.ScanStruct(&step); err != nil {
			return nil, fmt.Errorf("scan attempt step: %w", err)
		}
		steps = append(steps, step)
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("read attempt steps: %w", err)
	}
	return steps, nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
