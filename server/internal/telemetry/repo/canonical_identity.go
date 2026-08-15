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
	// lowerUTF8, not lower: the map keys are built with Postgres lower(),
	// which is Unicode-aware; ClickHouse lower() only folds ASCII and would
	// split any non-ASCII-cased email into a permanent literal bucket.
	get := "joinGet('identity_map', 'canonical_email', " + orgLiteral + ", lowerUTF8(" + expr + "))"
	return "if(position(" + expr + ", '@') > 0, coalesce(nullIf(" + get + ", ''), lowerUTF8(" + expr + ")), " + expr + ")"
}

// canonicalEmailValueList renders the right-hand side of an IN comparison:
// one SQL fragment per requested email value, folded through the same joinGet
// the column side uses. Non-email values are returned separately so callers
// can match them with the literal path's case-insensitive semantics (the
// pre-fold code compared lower(col) to lowered values for hostnames too).
func canonicalEmailValueList(orgLiteral string, values []string) (fragments []string, args []any, plain []string) {
	fragments = make([]string, 0, len(values))
	args = make([]any, 0, len(values)*2)
	for _, value := range values {
		if !strings.Contains(value, "@") {
			plain = append(plain, strings.ToLower(value))
			continue
		}
		normalized := strings.ToLower(strings.TrimSpace(value))
		fragments = append(fragments, "coalesce(nullIf(joinGet('identity_map', 'canonical_email', "+orgLiteral+", ?), ''), ?)")
		args = append(args, normalized, normalized)
	}
	return fragments, args, plain
}

// canonicalEmailPredicate is the shared comparison: folded emails match the
// folded column; plain values (hostnames, the ” bucket) match the raw column
// case-insensitively, mirroring the literal path.
func canonicalEmailPredicate(orgLiteral, column string, values []string) squirrel.Sqlizer {
	fragments, args, plain := canonicalEmailValueList(orgLiteral, values)
	var preds squirrel.Or
	if len(fragments) > 0 {
		preds = append(preds, squirrel.Expr(canonicalEmailExpr(orgLiteral, column)+" IN ("+strings.Join(fragments, ", ")+")", args...))
	}
	if len(plain) > 0 {
		preds = append(preds, squirrel.Eq{"lowerUTF8(" + column + ")": plain})
	}
	if len(preds) == 0 {
		return squirrel.Expr("0")
	}
	return preds
}

// canonicalEmailFilter is the WHERE predicate for the email dimension on the
// aggregate path: canonical(column) IN (canonical(v1), ...). Preserves the
// literal "" bucket — an empty requested value binds as ” and an empty column
// value folds to itself.
func canonicalEmailFilter(orgLiteral, column string, values []string) squirrel.Sqlizer {
	return canonicalEmailPredicate(orgLiteral, column, values)
}

// canonicalScalarRowPredicate mirrors sessionScalarRowPredicate with the fold
// applied to both sides: a requested "" means "this row has an empty value".
func canonicalScalarRowPredicate(orgLiteral, expr string, values []string) squirrel.Sqlizer {
	hasEmpty, nonEmpty := splitEmptyValue(values)

	emptyPred := squirrel.Expr(expr + " = ''")
	if len(nonEmpty) == 0 {
		return emptyPred
	}
	nonEmptyPred := canonicalEmailPredicate(orgLiteral, expr, nonEmpty)
	if !hasEmpty {
		return nonEmptyPred
	}
	return squirrel.Or{nonEmptyPred, emptyPred}
}

// canonicalScalarHaving mirrors sessionScalarHaving with the fold applied to
// both sides: a chat matches when any row folds to a requested identity, and
// a requested "" matches chats with no non-empty value on any row.
func canonicalScalarHaving(orgLiteral, expr string, values []string) squirrel.Sqlizer {
	hasEmpty, nonEmpty := splitEmptyValue(values)

	emptyPred := squirrel.Expr("countIf(" + expr + " != '') = 0")
	if len(nonEmpty) == 0 {
		return emptyPred
	}
	inner, innerArgs, err := canonicalEmailPredicate(orgLiteral, expr, nonEmpty).ToSql()
	if err != nil {
		// squirrel expression building cannot fail for these shapes; match on
		// nothing rather than panic in a query builder.
		return squirrel.Expr("0")
	}
	nonEmptyPred := squirrel.Expr("countIf("+inner+") > 0", innerArgs...)
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
	fragments, args, plain := canonicalEmailValueList(orgLiteral, nonEmpty)
	var preds squirrel.Or
	if len(fragments) > 0 {
		preds = append(preds, squirrel.Expr("hasAny("+merged+", array("+strings.Join(fragments, ", ")+"))", args...))
	}
	if len(plain) > 0 {
		// Plain values match the raw elements case-insensitively, like the
		// literal path's arrayMap(lower) comparison.
		preds = append(preds, squirrel.Expr("hasAny(groupUniqArrayArray(arrayMap(x -> lowerUTF8(x), "+column+")), ?)", plain))
	}
	nonEmptyPred := squirrel.Sqlizer(preds)
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

// CanonicalUserIdentity identifies one employee for the ungrouped per-user
// queries in canonical fold mode. An email identifier resolves entirely
// in-query — both the email fold and the owning user id come from the same
// identity_map generation — while a user-id identifier matches user_id-keyed
// rows directly plus rows carrying the user's directory email (resolved by
// the caller with a single lookup). The zero value disables canonical
// matching (callers fall back to the literal UserIdentity path).
type CanonicalUserIdentity struct {
	OrgID      string
	UserID     string
	EmailLower string
}

// Enabled reports whether the identity can drive the canonical filter: it
// needs an org id that passes the SQL-literal allowlist plus at least one
// identity leg. Callers building the scope must check it — a disabled
// canonical identity applies no filter, so the service falls back to the
// legacy expanded scope rather than serving pages unfiltered.
func (c CanonicalUserIdentity) Enabled() bool {
	return canonicalIdentityOrgLiteral(c.OrgID) != "" && (c.UserID != "" || c.EmailLower != "")
}

// withCanonicalUserIdentityFilter mirrors withUserIdentityFilter's id-wins
// precedence: a row with a user_id is attributed by that id alone, and only
// email-less rows fall back to the folded email comparison — the DNO-509
// double-count guard, unchanged. The joinGet arm guards user_id != ” because
// an unmapped email folds to ” and must never sweep in email-less rows.
func withCanonicalUserIdentityFilter(sb squirrel.SelectBuilder, ident CanonicalUserIdentity) squirrel.SelectBuilder {
	orgLit := canonicalIdentityOrgLiteral(ident.OrgID)

	var match squirrel.Or
	switch {
	case ident.UserID != "":
		match = append(match, squirrel.Eq{"telemetry_logs.user_id": ident.UserID})
	case ident.EmailLower != "":
		match = append(match, squirrel.Expr(
			"(telemetry_logs.user_id != '' AND telemetry_logs.user_id = joinGet('identity_map', 'canonical_user_id', "+orgLit+", ?))",
			ident.EmailLower))
	}
	if ident.EmailLower != "" {
		fragments, args, _ := canonicalEmailValueList(orgLit, []string{ident.EmailLower})
		if len(fragments) > 0 {
			match = append(match, squirrel.Expr(
				"(telemetry_logs.user_id = '' AND "+canonicalEmailExpr(orgLit, "telemetry_logs.user_email")+" = "+fragments[0]+")",
				args...))
		}
	}
	return sb.Where(match)
}

// orgLit is the validated org literal when canonical matching is enabled, or
// "" — the disabled signal withCanonicalFoldSettings keys off.
func (c CanonicalUserIdentity) orgLit() string {
	if !c.Enabled() {
		return ""
	}
	return canonicalIdentityOrgLiteral(c.OrgID)
}
