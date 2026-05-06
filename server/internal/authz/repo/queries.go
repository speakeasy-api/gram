package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// InsertChallenge writes a single challenge row using server-side async insert.
// The call is fire-and-forget from CH's perspective: it acks once the row is
// queued in CH's async insert buffer, not once the row is committed to disk.
func (q *Queries) InsertChallenge(ctx context.Context, row ChallengeRow) error {
	ctx = clickhouse.Context(ctx,
		clickhouse.WithAsync(false),
		clickhouse.WithSettings(clickhouse.Settings{
			"async_insert":          1,
			"wait_for_async_insert": 0,
		}),
	)

	reqScope := make([]string, len(row.RequestedChecks))
	reqKind := make([]string, len(row.RequestedChecks))
	reqRID := make([]string, len(row.RequestedChecks))
	reqSel := make([]string, len(row.RequestedChecks))
	for i, c := range row.RequestedChecks {
		reqScope[i] = c.Scope
		reqKind[i] = c.ResourceKind
		reqRID[i] = c.ResourceID
		reqSel[i] = c.Selector
	}

	mgURN := make([]string, len(row.MatchedGrants))
	mgScope := make([]string, len(row.MatchedGrants))
	mgSel := make([]string, len(row.MatchedGrants))
	mgVia := make([]string, len(row.MatchedGrants))
	for i, g := range row.MatchedGrants {
		mgURN[i] = g.PrincipalURN
		mgScope[i] = g.Scope
		mgSel[i] = g.Selector
		mgVia[i] = g.MatchedViaCheckScope
	}

	const query = `INSERT INTO authz_challenges (
		id,
		timestamp,
		organization_id,
		project_id,
		trace_id,
		span_id,
		request_id,
		principal_urn,
		principal_type,
		user_id,
		user_external_id,
		user_email,
		api_key_id,
		session_id,
		role_slugs,
		operation,
		outcome,
		reason,
		scope,
		resource_kind,
		resource_id,
		selector,
		expanded_scopes,
		"requested_checks.scope",
		"requested_checks.resource_kind",
		"requested_checks.resource_id",
		"requested_checks.selector",
		"matched_grants.principal_urn",
		"matched_grants.scope",
		"matched_grants.selector",
		"matched_grants.matched_via_check_scope",
		evaluated_grant_count,
		filter_candidate_count,
		filter_allowed_count
	) VALUES (
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?
	)`

	if err := q.conn.Exec(ctx, query,
		row.ID,
		row.Timestamp,
		row.OrganizationID,
		row.ProjectID,
		row.TraceID,
		row.SpanID,
		row.RequestID,
		row.PrincipalURN,
		string(row.PrincipalType),
		row.UserID,
		row.UserExternalID,
		row.UserEmail,
		row.APIKeyID,
		row.SessionID,
		row.RoleSlugs,
		string(row.Operation),
		string(row.Outcome),
		string(row.Reason),
		row.Scope,
		row.ResourceKind,
		row.ResourceID,
		row.Selector,
		row.ExpandedScopes,
		reqScope,
		reqKind,
		reqRID,
		reqSel,
		mgURN,
		mgScope,
		mgSel,
		mgVia,
		row.EvaluatedGrantCount,
		row.FilterCandidateCount,
		row.FilterAllowedCount,
	); err != nil {
		return fmt.Errorf("exec authz challenge insert: %w", err)
	}
	return nil
}

// ChallengeListFilters controls which rows ListChallenges returns.
type ChallengeListFilters struct {
	OrganizationID string
	ProjectID      string // empty = no filter
	Outcome        string // empty = no filter
	PrincipalURN   string // empty = no filter
	Scope          string // empty = no filter
	Limit          uint64
	Offset         uint64
	SkipPagination bool // when true, omit LIMIT/OFFSET (used when resolved filter requires post-join pagination)
}

// challengeWhereClause builds a WHERE clause and args slice from ChallengeListFilters.
func challengeWhereClause(f ChallengeListFilters) (string, []any) {
	conditions := []string{"organization_id = ?"}
	args := []any{f.OrganizationID}

	if f.ProjectID != "" {
		conditions = append(conditions, "project_id = ?")
		args = append(args, f.ProjectID)
	}
	if f.Outcome != "" {
		conditions = append(conditions, "outcome = ?")
		args = append(args, f.Outcome)
	}
	if f.PrincipalURN != "" {
		conditions = append(conditions, "principal_urn = ?")
		args = append(args, f.PrincipalURN)
	}
	if f.Scope != "" {
		conditions = append(conditions, "scope = ?")
		args = append(args, f.Scope)
	}

	return strings.Join(conditions, " AND "), args
}

// ChallengeSummary is the subset of a challenge row returned by ListChallenges.
type ChallengeSummary struct {
	ID                  string
	Timestamp           string // RFC 3339
	OrganizationID      string
	ProjectID           string
	PrincipalURN        string
	PrincipalType       string
	UserID              *string
	UserEmail           *string
	Operation           string
	Outcome             string
	Reason              string
	Scope               string
	ResourceKind        string
	ResourceID          string
	RoleSlugs           []string
	EvaluatedGrantCount uint32
	MatchedGrantCount   uint64
}

// ListChallenges queries ClickHouse for authz challenge events.
func (q *Queries) ListChallenges(ctx context.Context, f ChallengeListFilters) ([]ChallengeSummary, error) {
	where, args := challengeWhereClause(f)
	query := `SELECT
		id,
		formatDateTime(timestamp, '%Y-%m-%dT%H:%i:%S.000Z', 'UTC') AS ts,
		organization_id,
		project_id,
		principal_urn,
		principal_type,
		user_id,
		user_email,
		operation,
		outcome,
		reason,
		scope,
		resource_kind,
		resource_id,
		role_slugs,
		evaluated_grant_count,
		length(matched_grants.scope) AS matched_grant_count
	FROM authz_challenges
	WHERE ` + where + `
	ORDER BY timestamp DESC`

	if !f.SkipPagination {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, f.Offset)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("exec list challenges: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var results []ChallengeSummary
	for rows.Next() {
		var r ChallengeSummary
		if err := rows.Scan(
			&r.ID,
			&r.Timestamp,
			&r.OrganizationID,
			&r.ProjectID,
			&r.PrincipalURN,
			&r.PrincipalType,
			&r.UserID,
			&r.UserEmail,
			&r.Operation,
			&r.Outcome,
			&r.Reason,
			&r.Scope,
			&r.ResourceKind,
			&r.ResourceID,
			&r.RoleSlugs,
			&r.EvaluatedGrantCount,
			&r.MatchedGrantCount,
		); err != nil {
			return nil, fmt.Errorf("scan challenge row: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate challenge rows: %w", err)
	}
	return results, nil
}

// CountChallenges returns the total number of matching challenges for pagination.
func (q *Queries) CountChallenges(ctx context.Context, f ChallengeListFilters) (uint64, error) {
	where, args := challengeWhereClause(f)
	query := "SELECT count(*) FROM authz_challenges WHERE " + where

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("exec count challenges: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var count uint64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("scan count: %w", err)
		}
	}
	return count, nil
}
