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

type Engine struct {
	env *cel.Env
}

// New builds the CEL environment — the single source of truth for what a
// spend rule target expression may reference.
func New() (*Engine, error) {
	env, err := cel.NewEnv(
		ext.Strings(),

		cel.Variable("email", cel.StringType),
		cel.Variable("department_name", cel.StringType),
		cel.Variable("job_title", cel.StringType),
		cel.Variable("employee_type", cel.StringType),
		cel.Variable("division_name", cel.StringType),
		cel.Variable("cost_center_name", cel.StringType),
		cel.Variable("groups", cel.ListType(cel.StringType)),
		cel.Variable("roles", cel.ListType(cel.StringType)),
		cel.Variable("spend_usd", cel.DoubleType),
		cel.Variable("limit_usd", cel.DoubleType),
		cel.Variable("used_pct", cel.DoubleType),
		cel.Variable("warn_at_pct", cel.DoubleType),
	)
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
