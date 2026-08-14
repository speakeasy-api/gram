package repo

// Canonical identity folding for the email dimension. The ClickHouse
// identity_map table (rebuilt from Postgres by the identity map sync worker)
// maps each unambiguous directory or linked-account email to its owner's
// directory email. Wrapping both sides of an email comparison — the column
// AND the requested filter values — in the same joinGet fold makes one
// employee's work, personal, and case-variant emails one identity, and makes
// every query self-consistent against map refresh lag: filter input and rows
// always resolve through the same map generation.
//
// Non-email values pass through untouched. The email dimension column doubles
// as a device-hostname bucket (see the registry comment on "email"), and
// hostnames must keep literal semantics; the position(_, '@') guard preserves
// them. Unmapped emails — no directory row, deliberately-ambiguous shared
// emails — fall back to lower(...), preserving the case-insensitive literal
// matching that predates the fold.

import (
	"regexp"
	"strings"

	"github.com/Masterminds/squirrel"
)

// identityOrgLiteralPattern is the allowlist for inlining an organization id
// into SQL expressions. Group-by expressions are assembled as plain strings,
// so the org id cannot be a bound parameter there; ids outside this charset
// (which never occur for real orgs) disable folding rather than risk a broken
// literal.
var identityOrgLiteralPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// canonicalIdentityOrgLiteral returns the quoted SQL literal for an org id,
// or "" when the id cannot be safely inlined (which disables folding).
func canonicalIdentityOrgLiteral(orgID string) string {
	if orgID == "" || !identityOrgLiteralPattern.MatchString(orgID) {
		return ""
	}
	return "'" + orgID + "'"
}

// canonicalEmailExpr wraps a String column/expression in the identity fold.
// Emails resolve to their canonical directory email (or lower(expr) when
// unmapped); values without an "@" — hostnames, empty strings — pass through
// verbatim. Folding never maps an empty value to a non-empty one or back, so
// "(unset)"-bucket semantics are unchanged.
func canonicalEmailExpr(orgLiteral, expr string) string {
	get := "joinGet('identity_map', 'canonical_email', " + orgLiteral + ", lower(" + expr + "))"
	return "if(position(" + expr + ", '@') > 0, coalesce(nullIf(" + get + ", ''), lower(" + expr + ")), " + expr + ")"
}

// canonicalEmailValueList renders the right-hand side of an IN comparison:
// one SQL fragment per requested value, folding email values through the same
// joinGet the column side uses and binding everything else literally. The
// email constants are normalized (lowercased, trimmed) in Go, so only the
// joinGet itself is left to ClickHouse.
func canonicalEmailValueList(orgLiteral string, values []string) (fragments []string, args []any) {
	fragments = make([]string, 0, len(values))
	args = make([]any, 0, len(values)*2)
	for _, value := range values {
		if !strings.Contains(value, "@") {
			fragments = append(fragments, "?")
			args = append(args, value)
			continue
		}
		normalized := strings.ToLower(strings.TrimSpace(value))
		fragments = append(fragments, "coalesce(nullIf(joinGet('identity_map', 'canonical_email', "+orgLiteral+", ?), ''), ?)")
		args = append(args, normalized, normalized)
	}
	return fragments, args
}

// canonicalEmailFilter is the WHERE predicate for the email dimension on the
// aggregate path: canonical(column) IN (canonical(v1), ...). Preserves the
// literal "" bucket — an empty requested value binds as ” and an empty column
// value folds to itself.
func canonicalEmailFilter(orgLiteral, column string, values []string) squirrel.Sqlizer {
	fragments, args := canonicalEmailValueList(orgLiteral, values)
	return squirrel.Expr(canonicalEmailExpr(orgLiteral, column)+" IN ("+strings.Join(fragments, ", ")+")", args...)
}

// canonicalScalarRowPredicate mirrors sessionScalarRowPredicate with the fold
// applied to both sides: a requested "" means "this row has an empty value".
func canonicalScalarRowPredicate(orgLiteral, expr string, values []string) squirrel.Sqlizer {
	folded := canonicalEmailExpr(orgLiteral, expr)
	hasEmpty, nonEmpty := splitEmptyValue(values)

	emptyPred := squirrel.Expr(expr + " = ''")
	if len(nonEmpty) == 0 {
		return emptyPred
	}
	fragments, args := canonicalEmailValueList(orgLiteral, nonEmpty)
	nonEmptyPred := squirrel.Expr(folded+" IN ("+strings.Join(fragments, ", ")+")", args...)
	if !hasEmpty {
		return nonEmptyPred
	}
	return squirrel.Or{nonEmptyPred, emptyPred}
}

// canonicalScalarHaving mirrors sessionScalarHaving with the fold applied to
// both sides: a chat matches when any row folds to a requested identity, and
// a requested "" matches chats with no non-empty value on any row.
func canonicalScalarHaving(orgLiteral, expr string, values []string) squirrel.Sqlizer {
	folded := canonicalEmailExpr(orgLiteral, expr)
	hasEmpty, nonEmpty := splitEmptyValue(values)

	emptyPred := squirrel.Expr("countIf(" + expr + " != '') = 0")
	if len(nonEmpty) == 0 {
		return emptyPred
	}
	fragments, args := canonicalEmailValueList(orgLiteral, nonEmpty)
	nonEmptyPred := squirrel.Expr("countIf("+folded+" IN ("+strings.Join(fragments, ", ")+")) > 0", args...)
	if !hasEmpty {
		return nonEmptyPred
	}
	return squirrel.Or{nonEmptyPred, emptyPred}
}

// canonicalSummaryValuesHaving mirrors sessionSummaryValuesHaving over the
// summary path's per-chat email arrays, folding each element and each
// requested value.
func canonicalSummaryValuesHaving(orgLiteral, column string, values []string) squirrel.Sqlizer {
	merged := "groupUniqArrayArray(arrayMap(x -> " + canonicalEmailExpr(orgLiteral, "x") + ", " + column + "))"
	hasEmpty, nonEmpty := splitEmptyValue(values)

	emptyPred := squirrel.Expr("NOT arrayExists(x -> x != '', " + merged + ")")
	if len(nonEmpty) == 0 {
		return emptyPred
	}
	fragments, args := canonicalEmailValueList(orgLiteral, nonEmpty)
	nonEmptyPred := squirrel.Expr("hasAny("+merged+", array("+strings.Join(fragments, ", ")+"))", args...)
	if !hasEmpty {
		return nonEmptyPred
	}
	return squirrel.Or{nonEmptyPred, emptyPred}
}

// splitEmptyValue separates the "(unset)" bucket request from real values,
// matching the split every session filter helper performs.
func splitEmptyValue(values []string) (hasEmpty bool, nonEmpty []string) {
	nonEmpty = make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			hasEmpty = true
			continue
		}
		nonEmpty = append(nonEmpty, v)
	}
	return hasEmpty, nonEmpty
}

// withCanonicalFoldSettings disables the query condition cache on folded
// queries. The cache stores per-granule WHERE results keyed by predicate, and
// a predicate containing joinGet over the mutable identity_map is not safely
// cacheable: after a map swap, cached granule verdicts from the previous
// generation would silently mis-filter rows until eviction.
func withCanonicalFoldSettings(sb squirrel.SelectBuilder, canonicalOrgLit string) squirrel.SelectBuilder {
	if canonicalOrgLit == "" {
		return sb
	}
	return sb.Suffix("SETTINGS use_query_condition_cache = 0")
}
