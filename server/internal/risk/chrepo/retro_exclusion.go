package chrepo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Retroactive exclusion propagation: risk_findings is append-only, and every
// read path resolves an id to its latest copy by inserted_at, so changing a
// finding's effective exclusion state means appending a copy of its latest
// row with excluded_at/exclusion_id replaced and everything else — including
// created_at and message_created_at — preserved verbatim. Copies land in the
// original's daily partition (same created_at) and expire with it under the
// table TTL. The queries here are the ClickHouse counterpart of the Postgres
// reconcile batches in server/internal/background/activities/risk_exclusion.
//
// All statements are scoped to one partition day and run synchronously (no
// async_insert): the reconcile activity iterates days with a heartbeat
// between statements, and a statement's return means the read paths already
// see the new state.
//
// The same append-a-flagged-copy projection is the intended mechanism for
// moving false-positive marking ClickHouse-native once Postgres risk_results
// is decommissioned.

// RetroExclusionScope bounds one statement to a tenant and one partition day
// ([DayStart, DayEnd) half-open).
type RetroExclusionScope struct {
	OrganizationID string
	ProjectID      string
	DayStart       time.Time
	DayEnd         time.Time
}

// RetroExclusionPredicate is the ClickHouse-evaluable half of an exclusion's
// matching rule. Exactly one of RuleID, Source, or TenantFingerprints is set
// (rule_id and entity_type exclusions resolve to RuleID; exact resolves to
// one fingerprint per pepper version). Regex exclusions are not expressible
// here — their candidates are listed via ListRetroRegexCandidates and matched
// in Go against plaintext reconstructed from the chat store.
//
// RuleIDFilter and SourceFilter apply to every match type, mirroring the
// scan-time ExclusionSet semantics (the documented contract), not the
// Postgres apply queries' historical asymmetry.
type RetroExclusionPredicate struct {
	PolicyID           string
	RuleID             string
	Source             string
	TenantFingerprints []string
	RuleIDFilter       string
	SourceFilter       string
}

// chTimeFormat renders a timestamp for string-binding into DateTime64(9)
// columns and comparisons. Bound as a string, not time.Time: the driver
// truncates a bound time.Time to whole seconds on this Exec path (see the
// InsertRiskFindings comment).
const chTimeFormat = "2006-01-02 15:04:05.999999999"

// FormatCHTime renders t for binding against DateTime64(9) columns.
func FormatCHTime(t time.Time) string {
	return t.UTC().Format(chTimeFormat)
}

// latestRiskFindingsSubquery is the shared latest-copy-per-id dedup base,
// bounded to one tenant + partition day. Callers append `rn = 1` plus their
// predicate. Args: organization_id, project_id, day start, day end.
const latestRiskFindingsSubquery = `(
	SELECT *, ROW_NUMBER() OVER (PARTITION BY id ORDER BY inserted_at DESC) AS rn
	FROM risk_findings
	WHERE organization_id = ? AND project_id = ?
	  AND created_at >= ? AND created_at < ?
) AS latest`

func (s RetroExclusionScope) args() []any {
	return []any{s.OrganizationID, s.ProjectID, FormatCHTime(s.DayStart), FormatCHTime(s.DayEnd)}
}

// applyConditions renders the WHERE tail (after `rn = 1`) for an apply
// statement and its args. Dead-letter rows never match (the Postgres
// counterpart's `found IS TRUE`), and rows already excluded — by any
// exclusion — are never re-flagged, which also makes re-runs append nothing.
func (p RetroExclusionPredicate) applyConditions() (string, []any, error) {
	conds := []string{"dead_letter_reason = ''", "excluded_at IS NULL"}
	args := []any{}

	if p.PolicyID != "" {
		conds = append(conds, "risk_policy_id = ?")
		args = append(args, p.PolicyID)
	}

	matchers := 0
	if p.RuleID != "" {
		matchers++
		conds = append(conds, "rule_id = ?")
		args = append(args, p.RuleID)
	}
	if p.Source != "" {
		matchers++
		conds = append(conds, "source = ?")
		args = append(args, p.Source)
	}
	if len(p.TenantFingerprints) > 0 {
		matchers++
		conds = append(conds, "fingerprint_tenant_hs256 IN ("+strings.TrimSuffix(strings.Repeat("?,", len(p.TenantFingerprints)), ",")+")")
		for _, fp := range p.TenantFingerprints {
			args = append(args, fp)
		}
	}
	if matchers != 1 {
		return "", nil, fmt.Errorf("retro exclusion predicate needs exactly one matcher, got %d", matchers)
	}

	if p.RuleIDFilter != "" {
		conds = append(conds, "rule_id = ?")
		args = append(args, p.RuleIDFilter)
	}
	if p.SourceFilter != "" {
		conds = append(conds, "source = ?")
		args = append(args, p.SourceFilter)
	}

	return strings.Join(conds, " AND "), args, nil
}

// copyProjection renders the INSERT ... SELECT column projection: every
// risk_findings column passed through verbatim except inserted_at (bound —
// the copy must sort after every prior copy of the id) and the two exclusion
// flags (bound or NULL per direction). Column order matches
// riskFindingColumns exactly, which a test pins against InsertRiskFindings.
func copyProjection(excludedAtExpr, exclusionIDExpr string) string {
	projected := make([]string, len(riskFindingColumns))
	for i, col := range riskFindingColumns {
		switch col {
		case "inserted_at":
			projected[i] = "?"
		case "excluded_at":
			projected[i] = excludedAtExpr
		case "exclusion_id":
			projected[i] = exclusionIDExpr
		default:
			projected[i] = col
		}
	}
	return strings.Join(projected, ", ")
}

// CountRetroExclusionApply returns how many latest-copy rows in scope the
// predicate would flag. Callers skip the append when zero and report the
// count as the rows-flagged metric (Exec has no affected-rows result).
func (q *Queries) CountRetroExclusionApply(ctx context.Context, scope RetroExclusionScope, p RetroExclusionPredicate) (uint64, error) {
	conds, condArgs, err := p.applyConditions()
	if err != nil {
		return 0, err
	}
	query := "SELECT count() FROM " + latestRiskFindingsSubquery + " WHERE rn = 1 AND " + conds
	args := append(scope.args(), condArgs...)

	count, err := q.scanCount(ctx, query, args)
	if err != nil {
		return 0, fmt.Errorf("count retro exclusion apply: %w", err)
	}
	return count, nil
}

// scanCount runs a single-value count() query through the CHTX Query method
// (the interface has no QueryRow).
func (q *Queries) scanCount(ctx context.Context, query string, args []any) (uint64, error) {
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("query count: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var count uint64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("scan count: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read count: %w", err)
	}
	return count, nil
}

// AppendRetroExclusionApply appends flagged copies of every latest-copy row
// in scope matching the predicate. excludedAt and insertedAt are
// FormatCHTime-rendered timestamps chosen by the caller (one fixed
// excludedAt per reconcile run; insertedAt strictly increasing across the
// run's statements).
func (q *Queries) AppendRetroExclusionApply(ctx context.Context, scope RetroExclusionScope, exclusionID uuid.UUID, excludedAt, insertedAt string, p RetroExclusionPredicate) error {
	conds, condArgs, err := p.applyConditions()
	if err != nil {
		return err
	}

	query := "INSERT INTO risk_findings (" + strings.Join(riskFindingColumns, ", ") + ") " +
		"SELECT " + copyProjection("?", "?") + " FROM " + latestRiskFindingsSubquery +
		" WHERE rn = 1 AND " + conds
	args := make([]any, 0, 7+len(condArgs))
	args = append(args, insertedAt, excludedAt, exclusionID)
	args = append(args, scope.args()...)
	args = append(args, condArgs...)

	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("append retro exclusion apply copies: %w", err)
	}
	return nil
}

// AppendRetroExclusionApplyByIDs is the apply used by the regex path: the
// caller has already matched plaintext in Go and hands over the winning ids.
// The excluded_at IS NULL guard keeps it idempotent under retries.
func (q *Queries) AppendRetroExclusionApplyByIDs(ctx context.Context, scope RetroExclusionScope, exclusionID uuid.UUID, excludedAt, insertedAt string, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	query := "INSERT INTO risk_findings (" + strings.Join(riskFindingColumns, ", ") + ") " +
		"SELECT " + copyProjection("?", "?") + " FROM " + latestRiskFindingsSubquery +
		" WHERE rn = 1 AND dead_letter_reason = '' AND excluded_at IS NULL AND id IN (" +
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
	args := make([]any, 0, 7+len(ids))
	args = append(args, insertedAt, excludedAt, exclusionID)
	args = append(args, scope.args()...)
	for _, id := range ids {
		args = append(args, id)
	}

	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("append retro exclusion apply copies by id: %w", err)
	}
	return nil
}

// CountRetroExclusionReversal returns how many latest-copy rows in scope are
// currently held by the exclusion.
func (q *Queries) CountRetroExclusionReversal(ctx context.Context, scope RetroExclusionScope, exclusionID uuid.UUID) (uint64, error) {
	query := "SELECT count() FROM " + latestRiskFindingsSubquery + " WHERE rn = 1 AND exclusion_id = ?"
	args := append(scope.args(), exclusionID)

	count, err := q.scanCount(ctx, query, args)
	if err != nil {
		return 0, fmt.Errorf("count retro exclusion reversal: %w", err)
	}
	return count, nil
}

// AppendRetroExclusionReversal appends un-flagged copies of every latest-copy
// row in scope held by the exclusion — including rows that were annotated at
// ingest time, which is what finally makes deleting or disabling an
// exclusion un-hide its findings.
func (q *Queries) AppendRetroExclusionReversal(ctx context.Context, scope RetroExclusionScope, exclusionID uuid.UUID, insertedAt string) error {
	query := "INSERT INTO risk_findings (" + strings.Join(riskFindingColumns, ", ") + ") " +
		"SELECT " + copyProjection("NULL", "NULL") + " FROM " + latestRiskFindingsSubquery +
		" WHERE rn = 1 AND exclusion_id = ?"
	args := make([]any, 0, 6)
	args = append(args, insertedAt)
	args = append(args, scope.args()...)
	args = append(args, exclusionID)

	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("append retro exclusion reversal copies: %w", err)
	}
	return nil
}

// RetroRegexCandidate is one latest-copy row eligible for regex matching:
// everything the reveal-match reconstruction needs to rebuild the plaintext
// from the chat store plus verify it against the stored length and
// fingerprints.
type RetroRegexCandidate struct {
	ID            uuid.UUID
	ChatMessageID string
	ContentPartID string
	ChatID        string
	Source        string
	RuleID        string
	Surface       string
	Field         string
	Path          string
	ToolCallID    string
	StartPos      int32
	EndPos        int32
	MatchLen      uint32
	MatchRedacted string
}

// ListRetroRegexCandidates returns the rows a regex exclusion must be
// evaluated against: latest copies in scope passing the shared filters that
// carry reconstructable match metadata (match_len > 0 and a known surface).
// Rows without it — judge verdicts, derived findings, pre-metadata rows —
// carry no match content anywhere, so skipping them is parity with the
// Postgres reconcile, whose NULL match never satisfies a regex either.
func (q *Queries) ListRetroRegexCandidates(ctx context.Context, scope RetroExclusionScope, p RetroExclusionPredicate) ([]RetroRegexCandidate, error) {
	conds := []string{"dead_letter_reason = ''", "excluded_at IS NULL", "match_len > 0", "surface != ''"}
	args := scope.args()

	if p.PolicyID != "" {
		conds = append(conds, "risk_policy_id = ?")
		args = append(args, p.PolicyID)
	}
	if p.RuleIDFilter != "" {
		conds = append(conds, "rule_id = ?")
		args = append(args, p.RuleIDFilter)
	}
	if p.SourceFilter != "" {
		conds = append(conds, "source = ?")
		args = append(args, p.SourceFilter)
	}

	query := `SELECT id, chat_message_id, content_part_id, chat_id, source, rule_id,
		surface, field, path, tool_call_id, start_pos, end_pos, match_len, match_redacted
	FROM ` + latestRiskFindingsSubquery + " WHERE rn = 1 AND " + strings.Join(conds, " AND ")

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list retro regex candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RetroRegexCandidate
	for rows.Next() {
		var c RetroRegexCandidate
		if err := rows.Scan(
			&c.ID,
			&c.ChatMessageID,
			&c.ContentPartID,
			&c.ChatID,
			&c.Source,
			&c.RuleID,
			&c.Surface,
			&c.Field,
			&c.Path,
			&c.ToolCallID,
			&c.StartPos,
			&c.EndPos,
			&c.MatchLen,
			&c.MatchRedacted,
		); err != nil {
			return nil, fmt.Errorf("scan retro regex candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read retro regex candidates: %w", err)
	}
	return out, nil
}
