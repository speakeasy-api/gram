package repo

import (
	"strings"

	"github.com/Masterminds/squirrel"
)

// OwnScope limits a raw log or trace read to the caller's own sessions and
// to agents they currently own. It is what a member sees without log:read.
// Unattributed rows are excluded. Request filters only narrow it further.
type OwnScope struct {
	// UserID matches user.id and gram.auth.user_id.
	UserID string
	// UserEmail matches user.email.
	UserEmail string
	// AgentIDs match the authenticated actor agent.
	AgentIDs []string
	// IdentityOrg, when set, folds the email match through identity_map.
	// Empty means a case-insensitive literal match.
	IdentityOrg string
}

// telemetryLogsPredicate scopes raw telemetry_logs rows. Legs are a flat OR: hosted MCP rows
// keep the external id in user.id, the member's email in user.email, and the internal id only in
// gram.auth.user_id. Columns are table-qualified so a per-chat aggregate aliasing user_id reads the column.
func (s *OwnScope) telemetryLogsPredicate() squirrel.Sqlizer {
	var preds squirrel.Or
	if s.UserID != "" {
		preds = append(preds,
			squirrel.Eq{"telemetry_logs.user_id": s.UserID},
			squirrel.Eq{chAttr("gram.auth.user_id"): s.UserID},
		)
	}
	if s.UserEmail != "" {
		preds = append(preds, s.emailPredicate("telemetry_logs.user_email"))
	}
	if len(s.AgentIDs) > 0 {
		// Same expression trace_summaries_mv derives agent_id from.
		preds = append(preds, squirrel.Eq{toolUsageAgentIDExpr: s.AgentIDs})
	}
	return failClosed(preds)
}

// traceSummariesPredicate scopes trace_summaries rows. The view has no
// gram.auth.user_id column, so an external-id-only row without email is
// missed here; adding that column to trace_summaries_mv closes the gap.
func (s *OwnScope) traceSummariesPredicate() squirrel.Sqlizer {
	var preds squirrel.Or
	if s.UserID != "" {
		preds = append(preds, squirrel.Eq{"user_id": s.UserID})
	}
	if s.UserEmail != "" {
		preds = append(preds, s.emailPredicate("user_email"))
	}
	if len(s.AgentIDs) > 0 {
		preds = append(preds, squirrel.Eq{"agent_id": s.AgentIDs})
	}
	return failClosed(preds)
}

// chatHasOwnRow admits a per-chat aggregate when any of the chat's rows is
// the member's own. Rows are not filtered individually: a session's tool-call
// rows often carry no identity, and dropping them would deflate its counts.
func (s *OwnScope) chatHasOwnRow() squirrel.Sqlizer {
	return squirrel.Expr("countIf(?) > 0", s.telemetryLogsPredicate())
}

// withFoldSettings adds the identity-fold query settings when needed.
func (s *OwnScope) withFoldSettings(sb squirrel.SelectBuilder) squirrel.SelectBuilder {
	return withCanonicalFoldSettings(sb, canonicalIdentityOrgLiteral(foldOrg("", s)))
}

// foldOrg picks the org a query folds under: the query's own canonical org
// when set, else the own scope's. A query folds at most once.
func foldOrg(canonicalIdentityOrg string, scope *OwnScope) string {
	if canonicalIdentityOrg != "" || scope == nil {
		return canonicalIdentityOrg
	}
	return scope.IdentityOrg
}

// emailPredicate folds through identity_map when the org has one, else
// matches the literal address case-insensitively. Unlike per-person filters,
// dropping the leg here would hide the member's own rows.
func (s *OwnScope) emailPredicate(column string) squirrel.Sqlizer {
	orgLit := canonicalIdentityOrgLiteral(s.IdentityOrg)
	if orgLit == "" {
		return squirrel.Eq{"lowerUTF8(" + column + ")": strings.ToLower(s.UserEmail)}
	}
	return canonicalEmailPredicate(orgLit, column, []string{s.UserEmail})
}

// failClosed matches no rows when no identity leg exists.
func failClosed(preds squirrel.Or) squirrel.Sqlizer {
	if len(preds) == 0 {
		return squirrel.Expr("0")
	}
	return preds
}
