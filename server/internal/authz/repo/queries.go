package repo

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
)

// sq is the squirrel statement builder pre-configured for ClickHouse (uses ? placeholders).
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

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
// Nil pointer fields are omitted from the WHERE clause.
type ChallengeListFilters struct {
	OrganizationID string
	ProjectID      *string
	Outcome        *string
	PrincipalURN   *string
	Scope          *string
	Limit          uint64
	Offset         uint64
	SkipPagination bool // when true, omit LIMIT/OFFSET (used when resolved filter requires post-join pagination)
}

// challengeWhere applies ChallengeListFilters to a squirrel SelectBuilder.
func challengeWhere(sb squirrel.SelectBuilder, f ChallengeListFilters) squirrel.SelectBuilder {
	sb = sb.Where("organization_id = ?", f.OrganizationID)
	if f.ProjectID != nil {
		sb = sb.Where("project_id = ?", *f.ProjectID)
	}
	if f.Outcome != nil {
		sb = sb.Where("outcome = ?", *f.Outcome)
	}
	if f.PrincipalURN != nil {
		sb = sb.Where("principal_urn = ?", *f.PrincipalURN)
	}
	if f.Scope != nil {
		sb = sb.Where("scope = ?", *f.Scope)
	}
	return sb
}

// challengePagination applies LIMIT/OFFSET to a squirrel SelectBuilder when not skipped.
func challengePagination(sb squirrel.SelectBuilder, f ChallengeListFilters) squirrel.SelectBuilder {
	if !f.SkipPagination {
		sb = sb.Limit(f.Limit).Offset(f.Offset)
	}
	return sb
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

var challengeSummaryColumns = []string{
	"id",
	"formatDateTime(timestamp, '%Y-%m-%dT%H:%i:%S.000Z', 'UTC') AS ts",
	"organization_id",
	"project_id",
	"principal_urn",
	"principal_type",
	"user_id",
	"user_email",
	"operation",
	"outcome",
	"reason",
	"scope",
	"resource_kind",
	"resource_id",
	"role_slugs",
	"evaluated_grant_count",
	"length(matched_grants.scope) AS matched_grant_count",
}

func scanChallengeSummary(rows interface{ Scan(dest ...any) error }) (ChallengeSummary, error) {
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
		return r, err
	}
	return r, nil
}

// ListChallenges queries ClickHouse for authz challenge events.
func (q *Queries) ListChallenges(ctx context.Context, f ChallengeListFilters) ([]ChallengeSummary, error) {
	sb := sq.Select(challengeSummaryColumns...).
		From("authz_challenges").
		OrderBy("timestamp DESC")
	sb = challengeWhere(sb, f)
	sb = challengePagination(sb, f)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list challenges query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("exec list challenges: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var results []ChallengeSummary
	for rows.Next() {
		r, err := scanChallengeSummary(rows)
		if err != nil {
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
	sb := sq.Select("count(*)").From("authz_challenges")
	sb = challengeWhere(sb, f)

	query, args, err := sb.ToSql()
	if err != nil {
		return 0, fmt.Errorf("build count challenges query: %w", err)
	}

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

// ListChallengesByIDs fetches full challenge rows for a set of IDs.
func (q *Queries) ListChallengesByIDs(ctx context.Context, orgID string, ids []string) ([]ChallengeSummary, error) {
	if len(ids) == 0 {
		return []ChallengeSummary{}, nil
	}

	sb := sq.Select(challengeSummaryColumns...).
		From("authz_challenges").
		Where("organization_id = ?", orgID).
		Where(squirrel.Eq{"id": ids}).
		OrderBy("timestamp DESC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list challenges by ids query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("exec list challenges by ids: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var results []ChallengeSummary
	for rows.Next() {
		r, err := scanChallengeSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan challenge by id row: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate challenges by ids: %w", err)
	}
	return results, nil
}

// ChallengeBucket is a group of challenges that share the same dimensions
// (principal, scope, outcome, resource) across all time.
type ChallengeBucket struct {
	// Representative fields (from the most recent challenge in the bucket).
	ID                  string
	LastSeen            string // RFC 3339
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

	// Bucket metadata.
	ChallengeCount uint64
	ChallengeIDs   []string
	FirstSeen      string // RFC 3339
}

var challengeBucketColumns = []string{
	"argMax(id, timestamp) AS rep_id",
	"formatDateTime(max(timestamp), '%Y-%m-%dT%H:%i:%S.000Z', 'UTC') AS last_seen",
	"organization_id",
	"project_id",
	"principal_urn",
	"argMax(principal_type, timestamp) AS principal_type",
	"argMax(user_id, timestamp) AS user_id",
	"argMax(user_email, timestamp) AS user_email",
	"argMax(operation, timestamp) AS operation",
	"outcome",
	"argMax(reason, timestamp) AS reason",
	"scope",
	"resource_kind",
	"resource_id",
	"argMax(role_slugs, timestamp) AS role_slugs",
	"argMax(evaluated_grant_count, timestamp) AS evaluated_grant_count",
	"max(length(matched_grants.scope)) AS matched_grant_count",
	"count(*) AS challenge_count",
	"arrayMap(x -> toString(x), groupArray(id)) AS challenge_ids",
	"formatDateTime(min(timestamp), '%Y-%m-%dT%H:%i:%S.000Z', 'UTC') AS first_seen",
}

const challengeBucketGroupBy = "organization_id, project_id, principal_urn, scope, outcome, resource_kind, resource_id"

// ListChallengeBuckets returns challenges grouped by dimensions, paginated.
func (q *Queries) ListChallengeBuckets(ctx context.Context, f ChallengeListFilters) ([]ChallengeBucket, error) {
	sb := sq.Select(challengeBucketColumns...).
		From("authz_challenges").
		GroupBy(challengeBucketGroupBy).
		OrderBy("max(timestamp) DESC")
	sb = challengeWhere(sb, f)
	sb = challengePagination(sb, f)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list challenge buckets query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("exec list challenge buckets: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var results []ChallengeBucket
	for rows.Next() {
		var r ChallengeBucket
		if err := rows.Scan(
			&r.ID,
			&r.LastSeen,
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
			&r.ChallengeCount,
			&r.ChallengeIDs,
			&r.FirstSeen,
		); err != nil {
			return nil, fmt.Errorf("scan challenge bucket row: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate challenge bucket rows: %w", err)
	}
	return results, nil
}

// CountChallengeBuckets returns the total number of dimension groups for pagination.
func (q *Queries) CountChallengeBuckets(ctx context.Context, f ChallengeListFilters) (uint64, error) {
	inner := sq.Select("1").
		From("authz_challenges").
		GroupBy(challengeBucketGroupBy)
	inner = challengeWhere(inner, f)

	innerQuery, args, err := inner.ToSql()
	if err != nil {
		return 0, fmt.Errorf("build count challenge buckets query: %w", err)
	}

	// Wrap in outer SELECT count(*) — squirrel doesn't natively support subquery counts.
	query := "SELECT count(*) FROM (" + innerQuery + ")"

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("exec count challenge buckets: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var count uint64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("scan bucket count: %w", err)
		}
	}
	return count, nil
}
