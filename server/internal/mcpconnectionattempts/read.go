package mcpconnectionattempts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// attemptEndingColumns are the columns that decide how an attempt ended, plus its
// provider (the one on the ending row, else the earliest) and weight. They reduce an
// attempt's rows in ClickHouse as Summarize does in Go, with the same tie-breaks on equal times.
const attemptEndingColumns = `
	organization_id,
	attempt_id,
	min(attempt_started_at) AS started_at,
	toBool(countIf(is_final) > 0) AS has_final,
	toString(argMinIf((step, outcome, reason), (event_time, step, outcome, reason), is_final).1) AS final_step,
	toString(argMinIf((step, outcome, reason), (event_time, step, outcome, reason), is_final).2) AS final_outcome,
	toString(argMinIf((step, outcome, reason), (event_time, step, outcome, reason), is_final).3) AS final_reason,
	minIf(event_time, is_final) AS final_at,
	toString(argMax((step, reason), (event_time, step, reason)).1) AS last_step,
	toString(argMax((step, reason), (event_time, step, reason)).2) AS last_reason,
	max(event_time) AS last_at,
	ifNull(nullIf(if(countIf(is_final) > 0,
			argMinIf(provider_host, (event_time, step, outcome, reason, provider_host), is_final),
			argMax(provider_host, (event_time, step, reason, provider_host))), ''),
		argMinIf(provider_host, (event_time, provider_host), provider_host != '')) AS provider,
	toUInt64(max(repeats)) + 1 AS weight`

// attemptColumns reduces an attempt's rows to one AttemptSummary.
const attemptColumns = attemptEndingColumns + `,
	argMinIf(subject_urn, (event_time, subject_urn), subject_urn != '') AS subject,
	any(app_key) AS app,
	any(server_id) AS server,
	any(server_kind) AS kind,
	any(project_id) AS project,
	argMinIf(remote_session_client_id, (event_time, remote_session_client_id), remote_session_client_id != '') AS remote_client,
	toString(if(countIf(client_family NOT IN ('', 'browser')) > 0,
		argMinIf(client_family, (event_time, client_family), client_family NOT IN ('', 'browser')),
		if(countIf(client_family = 'browser') > 0, 'browser', ''))) AS client,
	toBool(countIf(step_result = 'signed_in') > 0) AS signed_in,
	arraySort(arrayFilter(x -> x != '', arrayDistinct(arrayConcat(
		groupUniqArray(server_id), groupUniqArray(member_id), groupUniqArray(auth_client_id),
		groupUniqArray(remote_session_issuer_id), groupUniqArray(user_session_issuer_id),
		groupUniqArray(remote_session_client_id))))) AS touched`

// ExcludedOrganizations are left out of cross-organization reads: the demo organization's attempts are made up.
func ExcludedOrganizations() []string {
	return []string{constants.DemoOrganizationID}
}

// AttemptFilter selects whole attempts. Every field is the same on all of an
// attempt's rows, so no filter can drop the row that decides how an attempt ended.
type AttemptFilter struct {
	// StartedFrom bounds attempt start, inclusive. Required.
	StartedFrom time.Time

	// StartedBefore bounds attempt start, exclusive. Required.
	StartedBefore time.Time

	// OrganizationID keeps one organization, or every one when empty.
	OrganizationID string

	// ExcludeOrganizationIDs drops these organizations.
	ExcludeOrganizationIDs []string

	// ProjectID keeps one project, or every one when nil.
	ProjectID uuid.UUID

	// ServerID keeps one server or gateway, or every one when empty.
	ServerID string

	// PeopleFailingFrom and PeopleFailingBefore, when set, keep only attempts by a
	// person with an attempt that started in this range and has a row with a reason.
	PeopleFailingFrom time.Time

	// PeopleFailingBefore is the exclusive end of PeopleFailingFrom's range.
	PeopleFailingBefore time.Time

	// FixCandidatesOnly keeps only attempts that ended connected or carry a reason: the
	// successes that can fix a failure and the failures that show it was not fixed yet.
	// It applies after the attempt is reduced, so it keeps every attempt End can call
	// connected, failed or blocked for any now.
	FixCandidatesOnly bool
}

func (f AttemptFilter) where() (string, []any, error) {
	if f.StartedFrom.IsZero() || f.StartedBefore.IsZero() {
		return "", nil, errors.New("attempt filter needs a start range")
	}
	if f.PeopleFailingFrom.IsZero() != f.PeopleFailingBefore.IsZero() {
		return "", nil, errors.New("attempt filter needs both ends of the people range")
	}
	conds := []string{"attempt_started_at >= fromUnixTimestamp64Nano(?)", "attempt_started_at < fromUnixTimestamp64Nano(?)"}
	args := []any{f.StartedFrom.UnixNano(), f.StartedBefore.UnixNano()}
	if f.OrganizationID != "" {
		conds, args = append(conds, "organization_id = ?"), append(args, f.OrganizationID)
	}
	if len(f.ExcludeOrganizationIDs) > 0 {
		conds, args = append(conds, "NOT has(?, organization_id)"), append(args, f.ExcludeOrganizationIDs)
	}
	if f.ProjectID != uuid.Nil {
		conds, args = append(conds, "project_id = ?"), append(args, f.ProjectID)
	}
	if f.ServerID != "" {
		conds, args = append(conds, "server_id = ?"), append(args, f.ServerID)
	}
	if !f.PeopleFailingFrom.IsZero() {
		conds = append(conds, `(organization_id, subject_urn) IN (
		SELECT organization_id, subject_urn FROM `+eventsTable+`
		WHERE attempt_started_at >= fromUnixTimestamp64Nano(?) AND attempt_started_at < fromUnixTimestamp64Nano(?)
			AND subject_urn != ''
			AND attempt_id IN (
				SELECT attempt_id FROM `+eventsTable+`
				WHERE attempt_started_at >= fromUnixTimestamp64Nano(?) AND attempt_started_at < fromUnixTimestamp64Nano(?) AND reason != ''))`)
		from, before := f.PeopleFailingFrom.UnixNano(), f.PeopleFailingBefore.UnixNano()
		args = append(args, from, before, from, before)
		// A row without the person still belongs to the attempt, so select whole attempts.
		inner := strings.Join(conds, " AND ")
		return `attempt_id IN (SELECT attempt_id FROM ` + eventsTable + ` WHERE ` + inner + `) AND ` + strings.Join(conds[:2], " AND "), append(args, f.StartedFrom.UnixNano(), f.StartedBefore.UnixNano()), nil
	}
	return strings.Join(conds, " AND "), args, nil
}

// ReadAttempts summarizes every attempt the filter selects, calling each once per attempt.
func ReadAttempts(ctx context.Context, ch driver.Conn, filter AttemptFilter, each func(AttemptSummary)) error {
	where, args, err := filter.where()
	if err != nil {
		return err
	}
	having := ""
	if filter.FixCandidatesOnly {
		having = ` HAVING (has_final AND (final_outcome = '` + string(OutcomeConnected) + `' OR final_reason != '')) OR (NOT has_final AND last_reason != '')`
	}
	rows, err := ch.Query(ctx, `SELECT`+attemptColumns+` FROM `+eventsTable+` WHERE `+where+` GROUP BY organization_id, attempt_id`+having, args...)
	if err != nil {
		return fmt.Errorf("query attempts: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	for rows.Next() {
		var a AttemptSummary
		if err := rows.ScanStruct(&a); err != nil {
			return fmt.Errorf("scan attempt: %w", err)
		}
		each(a)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read attempt rows: %w", err)
	}
	return nil
}
