// Package celenv defines the single CEL environment for spend rule target
// expressions. Every expression is a boolean predicate over one actor's
// directory-synced attributes; an actor is in a rule's audience when the
// predicate evaluates true.
//
// Variables mirror the telemetry directory-attribute allowlist (see
// server/internal/telemetry/user_info_resolver.go): email, department_name,
// job_title, employee_type, division_name, cost_center_name (strings), and
// groups (list of directory group names). Standard CEL string functions
// (contains, startsWith, endsWith, matches) and list membership (`in`) are
// available via the strings extension and the core language.
package celenv

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/ext"
)

// Actor is the directory-derived view of one person a target expression
// evaluates against. Unset attributes are empty strings so expressions can
// compare without null handling.
type Actor struct {
	Email          string
	DepartmentName string
	JobTitle       string
	EmployeeType   string
	DivisionName   string
	CostCenterName string
	Groups         []string
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

// Eval evaluates a compiled target expression against one actor and returns
// whether the actor is in the rule's audience.
func (e *Engine) Eval(prg cel.Program, actor Actor) (bool, error) {
	groups := make([]string, 0, len(actor.Groups))
	groups = append(groups, actor.Groups...)

	out, _, err := prg.Eval(map[string]any{
		"email":            actor.Email,
		"department_name":  actor.DepartmentName,
		"job_title":        actor.JobTitle,
		"employee_type":    actor.EmployeeType,
		"division_name":    actor.DivisionName,
		"cost_center_name": actor.CostCenterName,
		"groups":           groups,
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
