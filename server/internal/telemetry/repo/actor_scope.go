package repo

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Masterminds/squirrel"
)

// ActorScope restricts a telemetry read to the activity of a fixed set of
// actors. It is how a logs:read grant narrowed to an actor department, group,
// or identity-provider role reaches the query layer: the service resolves the
// grant's dimensions to the members they cover and passes the resulting
// identities down.
//
// A nil *ActorScope means unrestricted — the caller holds log access over the
// whole organization. A non-nil scope always filters: an empty one matches
// nothing, so a grant that resolves to no members fails closed instead of
// widening.
type ActorScope struct {
	// Emails are the lowercase emails of the covered actors.
	Emails []string

	// UserIDs are the Gram user ids of the covered actors. Rows carrying a
	// user id are matched on it directly, which also covers activity recorded
	// before the actor's email was known.
	UserIDs []string
}

// actorScopePredicate builds the WHERE predicate for a scope against one
// table's actor identity columns. emailColumn is required; userIDColumn may be
// empty for sources that do not carry one. canonicalOrgLit folds the email
// comparison through identity_map so an employee's linked personal emails
// resolve to the same identity the grant covers.
func actorScopePredicate(scope *ActorScope, emailColumn, userIDColumn, canonicalOrgLit string) squirrel.Sqlizer {
	if scope == nil {
		return nil
	}

	var preds squirrel.Or
	if userIDColumn != "" && len(scope.UserIDs) > 0 {
		preds = append(preds, squirrel.Eq{userIDColumn: scope.UserIDs})
	}
	if emailColumn != "" && len(scope.Emails) > 0 {
		if canonicalOrgLit != "" {
			preds = append(preds, canonicalEmailPredicate(canonicalOrgLit, emailColumn, scope.Emails))
		} else {
			preds = append(preds, squirrel.Eq{"lowerUTF8(" + emailColumn + ")": scope.Emails})
		}
	}
	if len(preds) == 0 {
		return squirrel.Expr("0")
	}
	return preds
}

// withActorScope applies the scope to a builder, leaving it untouched when the
// read is unrestricted.
func withActorScope(sb squirrel.SelectBuilder, scope *ActorScope, emailColumn, userIDColumn, canonicalOrgLit string) squirrel.SelectBuilder {
	pred := actorScopePredicate(scope, emailColumn, userIDColumn, canonicalOrgLit)
	if pred == nil {
		return sb
	}
	return sb.Where(pred)
}

// withActorScopeTypedUser applies the scope to a read whose actor identity is
// projected as a typed (user_kind, user_key) pair — the shape the tool usage
// and hook trace queries normalize to. An external user id never matches: a
// grant covers directory members, and an end user of a customer's own product
// is not one.
func withActorScopeTypedUser(sb squirrel.SelectBuilder, scope *ActorScope) squirrel.SelectBuilder {
	if scope == nil {
		return sb
	}

	var preds squirrel.Or
	if len(scope.Emails) > 0 {
		preds = append(preds, squirrel.And{
			squirrel.Eq{"user_kind": toolUsageUserKindEmail},
			squirrel.Eq{"lowerUTF8(user_key)": scope.Emails},
		})
	}
	if len(scope.UserIDs) > 0 {
		preds = append(preds, squirrel.And{
			squirrel.Eq{"user_kind": toolUsageUserKindUserID},
			squirrel.Eq{"user_key": scope.UserIDs},
		})
	}
	if len(preds) == 0 {
		return sb.Where(squirrel.Expr("0"))
	}
	return sb.Where(preds)
}

// actorScopeEmailFilter expresses the scope as an email-dimension filter for
// the reads that already attribute activity through that dimension. Reusing it
// keeps one implementation of per-chat email matching, identity folding
// included, instead of a second predicate that could disagree with it.
//
// Reports false when the scope covers nobody, which no filter can express:
// dropping it would widen the read, so the caller returns an empty result.
func actorScopeEmailFilter(filters []AttributeMetricsFilter, scope *ActorScope) ([]AttributeMetricsFilter, bool) {
	if scope == nil {
		return filters, true
	}
	if len(scope.Emails) == 0 {
		return nil, false
	}
	return append(slices.Clone(filters), AttributeMetricsFilter{Dimension: "email", Values: scope.Emails}), true
}

// withActorScopeHaving applies the scope to a grouped read whose actor
// identity is only known per group (one chat spans many rows): a group is in
// scope when any of its rows is attributed to a covered actor.
func withActorScopeHaving(sb squirrel.SelectBuilder, scope *ActorScope, emailColumn, userIDColumn, canonicalOrgLit string) (squirrel.SelectBuilder, error) {
	pred := actorScopePredicate(scope, emailColumn, userIDColumn, canonicalOrgLit)
	if pred == nil {
		return sb, nil
	}
	inner, args, err := pred.ToSql()
	if err != nil {
		return sb, fmt.Errorf("build actor scope predicate: %w", err)
	}
	return sb.Having(squirrel.Expr("countIf("+inner+") > 0", args...)), nil
}

// errActorScopeCoversNobody signals that an actor scope resolved to no
// covered actors, so the read has no rows to return. Builders that assemble
// SQL as strings return it instead of emitting an unsatisfiable query.
var errActorScopeCoversNobody = errors.New("actor scope covers no actors")
