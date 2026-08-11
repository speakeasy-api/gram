// Package celenv defines the single CEL environment for spend rule
// expressions. Every expression is a boolean predicate over one actor — an
// organization member enriched with whatever we know about them — plus that
// actor's current usage against the rule.
//
// Variables cover three sources: identity (email), directory-synced
// attributes mirroring the telemetry allowlist (see
// server/internal/telemetry/user_info_resolver.go): department_name,
// job_title, employee_type, division_name, cost_center_name (strings) and
// groups (list of directory group names), and org membership (roles — the
// member's organization role slugs, e.g. "admin", "member"). Directory
// attributes are empty strings/lists for members without a synced directory
// profile. Usage fields are spend_usd, limit_usd, used_pct, and warn_at_pct.
// Standard CEL string functions (contains, startsWith, endsWith, matches) and
// list membership (`in`) are available via the strings extension and the core
// language.
package celenv

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/ext"
)

// Actor is the attribute view of one organization member a target expression
// evaluates against. Unset attributes are empty strings/lists so expressions
// can compare without null handling.
type Actor struct {
	Email          string
	DepartmentName string
	JobTitle       string
	EmployeeType   string
	DivisionName   string
	CostCenterName string
	Groups         []string
	Roles          []string
	SpendUSD       float64
	LimitUSD       float64
	UsedPct        float64
	WarnAtPct      float64
}

// AttributeKind is the value kind of an actor attribute: a scalar string or a
// list of strings. It drives both the CEL variable type and the operators a
// target condition may use against the attribute.
type AttributeKind string

const (
	KindString AttributeKind = "string"
	KindList   AttributeKind = "list"
)

// ActorAttribute is one member attribute a target condition can reference.
type ActorAttribute struct {
	Name        string
	Kind        AttributeKind
	Description string
}

// ActorAttributes is the catalog of member attributes a spend rule target
// condition may be written against — the single source of truth shared by the
// CEL environment (below), target-condition validation
// (spendrules.targetConditionExpr), and the listActorAttributes API that feeds
// the dashboard rule editor. Order is preserved in the API response, so it
// doubles as the editor's attribute ordering.
//
// Directory attributes mirror the telemetry allowlist (see
// server/internal/telemetry/user_info_resolver.go). Add an attribute here and
// it propagates to the CEL env, validation, the API, and the editor at once.
var ActorAttributes = []ActorAttribute{
	{Name: "department_name", Kind: KindString, Description: "Directory department the member belongs to."},
	{Name: "job_title", Kind: KindString, Description: "Directory job title."},
	{Name: "employee_type", Kind: KindString, Description: "Employment classification."},
	{Name: "division_name", Kind: KindString, Description: "Directory division or business unit."},
	{Name: "cost_center_name", Kind: KindString, Description: "Finance cost center the member rolls up to."},
	{Name: "email", Kind: KindString, Description: "Member email address."},
	{Name: "groups", Kind: KindList, Description: "IdP or directory group memberships."},
	{Name: "roles", Kind: KindList, Description: "Organization role slugs the member holds."},
}

// TargetAttributeKind returns the value kind of a target attribute and whether
// it is a known attribute.
func TargetAttributeKind(name string) (AttributeKind, bool) {
	for _, a := range ActorAttributes {
		if a.Name == name {
			return a.Kind, true
		}
	}
	return "", false
}

type Engine struct {
	env *cel.Env
}

// New builds the CEL environment — the single source of truth for what a
// spend rule target expression may reference. Member-attribute variables are
// derived from ActorAttributes; usage variables (spend/limit/pct) are fixed.
func New() (*Engine, error) {
	opts := []cel.EnvOption{ext.Strings()}
	for _, a := range ActorAttributes {
		switch a.Kind {
		case KindList:
			opts = append(opts, cel.Variable(a.Name, cel.ListType(cel.StringType)))
		case KindString:
			fallthrough
		default:
			opts = append(opts, cel.Variable(a.Name, cel.StringType))
		}
	}
	opts = append(opts,
		cel.Variable("spend_usd", cel.DoubleType),
		cel.Variable("limit_usd", cel.DoubleType),
		cel.Variable("used_pct", cel.DoubleType),
		cel.Variable("warn_at_pct", cel.DoubleType),
	)

	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("build spend rule cel env: %w", err)
	}
	return &Engine{env: env}, nil
}

// Compile type-checks a target expression and asserts it is a boolean
// predicate. Use at rule create/update time for validation and before
// evaluation.
func (e *Engine) Compile(expr string) (cel.Program, error) {
	ast, iss := e.env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("compile %q: %w", expr, iss.Err())
	}
	if out := ast.OutputType(); !out.IsExactType(cel.BoolType) {
		return nil, fmt.Errorf("expression must evaluate to bool, got %s", out)
	}
	prg, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("program %q: %w", expr, err)
	}
	return prg, nil
}

// Eval evaluates a compiled expression against one actor/usage view.
func (e *Engine) Eval(prg cel.Program, actor Actor) (bool, error) {
	groups := make([]string, 0, len(actor.Groups))
	groups = append(groups, actor.Groups...)
	roles := make([]string, 0, len(actor.Roles))
	roles = append(roles, actor.Roles...)

	out, _, err := prg.Eval(map[string]any{
		"email":            actor.Email,
		"department_name":  actor.DepartmentName,
		"job_title":        actor.JobTitle,
		"employee_type":    actor.EmployeeType,
		"division_name":    actor.DivisionName,
		"cost_center_name": actor.CostCenterName,
		"groups":           groups,
		"roles":            roles,
		"spend_usd":        actor.SpendUSD,
		"limit_usd":        actor.LimitUSD,
		"used_pct":         actor.UsedPct,
		"warn_at_pct":      actor.WarnAtPct,
	})
	if err != nil {
		return false, fmt.Errorf("eval target expression: %w", err)
	}
	b, ok := out.(types.Bool)
	if !ok {
		return false, fmt.Errorf("target expression evaluated to %s, want bool", out.Type())
	}
	return bool(b), nil
}
