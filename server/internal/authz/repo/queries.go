package repo

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
)

// sq is the squirrel statement builder pre-configured for ClickHouse (uses ? placeholders).
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// InsertChallenge writes one challenge with a stable deduplication token. The
// server may batch concurrent async inserts, but the call waits for the flush so
// the Pub/Sub handler only acknowledges a durably accepted row.
func (q *Queries) InsertChallenge(ctx context.Context, row ChallengeRow) error {
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert":               1,
		"async_insert_deduplicate":   1,
		"insert_deduplication_token": "authz-challenge:" + row.ID,
		"wait_for_async_insert":      1,
	}))

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
	Resolved       *bool

	// ResolvedChallengeIDs identifies challenge rows with a resolution record.
	// Bucket queries compare these IDs with each bucket's representative row.
	ResolvedChallengeIDs []string

	Limit          uint64
	Offset         uint64
	SkipPagination bool // when true, omit LIMIT/OFFSET (used when resolved filter requires post-join pagination)

	// MemberUserIDs, when non-nil, suppresses challenges raised by users outside
	// the organization (e.g. Speakeasy staff impersonating a customer org). Rows
	// are kept only when user_id is NULL (non-user principals such as api keys and
	// assistants) or user_id is one of these active org-member IDs. An empty
	// non-nil slice keeps only the NULL-user_id rows.
	MemberUserIDs []string
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
	if f.MemberUserIDs != nil {
		// Keep non-user principals (user_id IS NULL) plus active org members.
		// squirrel.Eq with an empty slice renders as a false predicate, so an
		// empty MemberUserIDs collapses this to "user_id IS NULL".
		//
		// The column is qualified with the table name because the bucket query
		// aliases argMax(user_id, timestamp) AS user_id, and ClickHouse resolves
		// a bare user_id in WHERE to that aggregate alias (illegal in WHERE).
		sb = sb.Where(squirrel.Or{
			squirrel.Expr("authz_challenges.user_id IS NULL"),
			squirrel.Eq{"authz_challenges.user_id": f.MemberUserIDs},
		})
	}
	return sb
}

// challengeBucketWhere applies filters to the pre-aggregated challenge bucket
// summary. Unattributed challenge rows are excluded when the summary is built.
func challengeBucketWhere(sb squirrel.SelectBuilder, f ChallengeListFilters) squirrel.SelectBuilder {
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
	if f.MemberUserIDs != nil {
		sb = sb.Where(squirrel.Or{
			squirrel.Eq{"user_id_filter": ""},
			squirrel.Eq{"user_id_filter": f.MemberUserIDs},
		})
	}
	return sb
}

// challengeBucketResolution filters grouped buckets by whether their most
// recent challenge has a resolution record in PostgreSQL.
func challengeBucketResolution(sb squirrel.SelectBuilder, f ChallengeListFilters, representativeID string) squirrel.SelectBuilder {
	if f.Resolved == nil {
		return sb
	}

	if len(f.ResolvedChallengeIDs) == 0 {
		if *f.Resolved {
			return sb.Having("0")
		}
		return sb
	}

	if *f.Resolved {
		return sb.Having(squirrel.Eq{representativeID: f.ResolvedChallengeIDs})
	}
	return sb.Having(squirrel.NotEq{representativeID: f.ResolvedChallengeIDs})
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
		return r, fmt.Errorf("scanning challenge summary: %w", err)
	}
	return r, nil
}

// ListChallenges queries ClickHouse for authz challenge events.
func (q *Queries) ListChallenges(ctx context.Context, f ChallengeListFilters) ([]ChallengeSummary, error) {
	sb := sq.Select(challengeSummaryColumns...).
		Distinct().
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
	sb := sq.Select("uniqExact(id)").From("authz_challenges")
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
		Distinct().
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
	"argMaxMerge(representative_id) AS bucket_id",
	"formatDateTime(max(last_seen), '%Y-%m-%dT%H:%i:%S.000Z', 'UTC') AS bucket_last_seen",
	"organization_id",
	"project_id",
	"principal_urn",
	"argMaxMerge(principal_type) AS bucket_principal_type",
	"argMaxMerge(user_id) AS bucket_user_id",
	"argMaxMerge(user_email) AS bucket_user_email",
	"argMaxMerge(operation) AS bucket_operation",
	"outcome",
	"argMaxMerge(reason) AS bucket_reason",
	"scope",
	"resource_kind",
	"resource_id",
	"argMaxMerge(role_slugs) AS bucket_role_slugs",
	"argMaxMerge(evaluated_grant_count) AS bucket_evaluated_grant_count",
	"max(matched_grant_count) AS bucket_matched_grant_count",
	"uniqExactMerge(challenge_count) AS bucket_challenge_count",
	"arrayMap(x -> toString(x), groupUniqArrayMerge(challenge_ids)) AS bucket_challenge_ids",
	"formatDateTime(min(first_seen), '%Y-%m-%dT%H:%i:%S.000Z', 'UTC') AS bucket_first_seen",
	"count() OVER () AS total_count",
}

const challengeBucketGroupBy = "organization_id, project_id, principal_urn, scope, outcome, resource_kind, resource_id"

// ListChallengeBuckets returns challenges grouped by dimensions, paginated.
func (q *Queries) ListChallengeBuckets(ctx context.Context, f ChallengeListFilters) ([]ChallengeBucket, uint64, error) {
	sb := sq.Select(challengeBucketColumns...).
		From("authz_challenge_bucket_summaries").
		GroupBy(challengeBucketGroupBy).
		OrderBy("bucket_last_seen DESC")
	sb = challengeBucketWhere(sb, f)
	sb = challengeBucketResolution(sb, f, "argMaxMerge(representative_id)")
	sb = challengePagination(sb, f)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("build list challenge buckets query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("exec list challenge buckets: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var results []ChallengeBucket
	var total uint64
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
			&total,
		); err != nil {
			return nil, 0, fmt.Errorf("scan challenge bucket row: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate challenge bucket rows: %w", err)
	}

	// A nonzero offset can become stale if buckets disappear between pages. The
	// window total is unavailable when LIMIT/OFFSET returns no rows, so retain a
	// count-only fallback for that uncommon case.
	if total == 0 && f.Offset > 0 {
		total, err = q.CountChallengeBuckets(ctx, f)
		if err != nil {
			return nil, 0, err
		}
	}
	return results, total, nil
}

// CountChallengeBuckets returns the total number of dimension groups for pagination.
func (q *Queries) CountChallengeBuckets(ctx context.Context, f ChallengeListFilters) (uint64, error) {
	inner := sq.Select("1").
		From("authz_challenge_bucket_summaries").
		GroupBy(challengeBucketGroupBy)
	inner = challengeBucketWhere(inner, f)
	inner = challengeBucketResolution(inner, f, "argMaxMerge(representative_id)")

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
