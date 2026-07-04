package spendrules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	"github.com/speakeasy-api/gram/server/internal/spendrules/repo"
)

// Rule status values derived from per-actor usage. flagging/blocking mean at
// least one matched actor is at or past the limit; approaching means at least
// one actor crossed the warning threshold.
const (
	StatusHealthy     = "healthy"
	StatusApproaching = "approaching"
	StatusFlagging    = "flagging"
	StatusBlocking    = "blocking"
)

// Rule actions.
const (
	ActionFlag  = "flag"
	ActionBlock = "block"
)

const DefaultRuleExpr = "spend_usd >= limit_usd"

// Event types recorded by the evaluator.
const (
	EventTypeWarning = "warning"
	EventTypeBreach  = "breach"
)

// Actor is one organization member: identity fields for reporting plus the
// CEL attribute view a target expression evaluates against — directory
// attributes and groups when a directory profile is synced, and the member's
// organization role slugs.
type Actor struct {
	UserID      string
	Email       string
	DisplayName string // empty when unknown
	Attrs       celenv.Actor
}

// ActorUsage is one matched actor's spend against a rule's per-person limit.
type ActorUsage struct {
	Actor    Actor
	SpendUSD float64
	LimitUSD float64
	UsedPct  float64
	Breached bool
}

// actorFromRow converts an org-member row into an Actor, decoding the
// allowlisted directory attributes out of the raw WorkOS custom-attributes
// payload. Non-string attribute values are ignored, matching the telemetry
// snapshot behaviour.
func actorFromRow(row repo.ListOrgActorsRow) Actor {
	var payload map[string]any
	if len(row.Attributes) > 0 {
		// Unmarshal errors leave the payload empty: a malformed attribute
		// blob should not exclude the actor from email/role-based targeting.
		_ = json.Unmarshal(row.Attributes, &payload)
	}
	str := func(key string) string {
		v, ok := payload[key].(string)
		if !ok {
			return ""
		}
		return v
	}

	return Actor{
		UserID:      row.UserID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Attrs: celenv.Actor{
			Email:          row.Email,
			DepartmentName: str("department_name"),
			JobTitle:       str("job_title"),
			EmployeeType:   str("employee_type"),
			DivisionName:   str("division_name"),
			CostCenterName: str("cost_center_name"),
			Groups:         row.GroupNames,
			Roles:          row.RoleSlugs,
			SpendUSD:       0,
			LimitUSD:       0,
			UsedPct:        0,
			WarnAtPct:      0,
		},
	}
}

// LoadActors reads the organization's members as Actors, enriched with their
// directory attributes and role slugs where available.
func LoadActors(ctx context.Context, queries *repo.Queries, organizationID string) ([]Actor, error) {
	rows, err := queries.ListOrgActors(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list org actors: %w", err)
	}
	actors := make([]Actor, 0, len(rows))
	for _, row := range rows {
		actors = append(actors, actorFromRow(row))
	}
	return actors, nil
}

// MatchActors compiles the target expression and returns the actors it
// matches. Per-actor evaluation errors fail the match: an expression that
// compiles against the env cannot error on plain string/list values, so any
// error is a bug worth surfacing rather than skipping.
func MatchActors(eng *celenv.Engine, targetExpr string, actors []Actor) ([]Actor, error) {
	prg, err := eng.Compile(targetExpr)
	if err != nil {
		return nil, fmt.Errorf("compile target expression: %w", err)
	}
	matched := make([]Actor, 0, len(actors))
	for _, actor := range actors {
		ok, err := eng.Eval(prg, actor.Attrs)
		if err != nil {
			return nil, fmt.Errorf("evaluate target expression for %s: %w", actor.Email, err)
		}
		if ok {
			matched = append(matched, actor)
		}
	}
	return matched, nil
}

// BuildActorUsages pairs matched actors with their spend (keyed by lowercase
// email) against a per-person limit, ordered by spend descending.
func BuildActorUsages(matched []Actor, spendByEmail map[string]float64, limitUSD float64) []ActorUsage {
	usages := make([]ActorUsage, 0, len(matched))
	for _, actor := range matched {
		spend := spendByEmail[conv.NormalizeEmail(actor.Email)]
		usedPct := 0.0
		if limitUSD > 0 {
			usedPct = spend / limitUSD * 100
		}
		usages = append(usages, ActorUsage{
			Actor:    actor,
			SpendUSD: spend,
			LimitUSD: limitUSD,
			UsedPct:  usedPct,
			Breached: false,
		})
	}
	sort.Slice(usages, func(i, j int) bool { return usages[i].SpendUSD > usages[j].SpendUSD })
	return usages
}

// EvalRuleUsages compiles the rule expression and marks each matched actor
// whose actor+usage view satisfies the rule. The expression is the budget
// breach predicate (for example: `spend_usd >= limit_usd`), while target_expr
// remains the audience predicate.
func EvalRuleUsages(eng *celenv.Engine, ruleExpr string, warnAtPct int32, usages []ActorUsage) ([]ActorUsage, error) {
	prg, err := eng.Compile(ruleExpr)
	if err != nil {
		return nil, fmt.Errorf("compile rule expression: %w", err)
	}
	for i := range usages {
		attrs := usages[i].Actor.Attrs
		attrs.SpendUSD = usages[i].SpendUSD
		attrs.LimitUSD = usages[i].LimitUSD
		attrs.UsedPct = usages[i].UsedPct
		attrs.WarnAtPct = float64(warnAtPct)
		ok, err := eng.Eval(prg, attrs)
		if err != nil {
			return nil, fmt.Errorf("evaluate rule expression for %s: %w", usages[i].Actor.Email, err)
		}
		usages[i].Breached = ok
	}
	return usages, nil
}

// RuleStatus derives the rule's display status from its action and the worst
// matched actor: any actor whose rule expression matched makes the rule
// flagging (action flag) or blocking (action block); any actor at/past the
// warning threshold makes it approaching; otherwise healthy.
func RuleStatus(action string, warnAtPct int32, usages []ActorUsage) string {
	breached := false
	warned := false
	for _, u := range usages {
		if u.Breached {
			breached = true
			break
		}
		if u.UsedPct >= float64(warnAtPct) {
			warned = true
		}
	}
	switch {
	case breached && action == ActionBlock:
		return StatusBlocking
	case breached:
		return StatusFlagging
	case warned:
		return StatusApproaching
	default:
		return StatusHealthy
	}
}
