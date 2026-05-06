package repo

import (
	"context"
	"fmt"

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
