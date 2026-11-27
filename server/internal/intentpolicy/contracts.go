package intentpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// contractCostLimit bounds one contract evaluation. The built-in contracts
// cost well under 1,000; the margin covers regex matching over long commands.
const contractCostLimit = 100_000

// inputRefPattern finds the label and tool paths a contract reads.
var inputRefPattern = regexp.MustCompile(`\b(label|rank|tool|action)\.([a-z_]+)(?:\.([a-z_]+))?`)

// Engine evaluates a guardrail's contracts.
type Engine struct {
	contracts []compiledContract
}

type compiledContract struct {
	contract Contract
	program  cel.Program
	inputs   []string
}

// NewEngine compiles every contract of the guardrail.
func NewEngine(g Guardrail) (*Engine, error) {
	env, err := cel.NewEnv(
		cel.Variable("label", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("rank", cel.MapType(cel.StringType, cel.IntType)),
		cel.Variable("tool", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("action", cel.MapType(cel.StringType, cel.DynType)),
		cel.Constant("PERMIT", cel.StringType, types.String(DecisionPermit)),
		cel.Constant("CLARIFY", cel.StringType, types.String(DecisionClarify)),
		cel.Constant("BLOCK", cel.StringType, types.String(DecisionBlock)),
		cel.Constant("ABSTAIN", cel.StringType, types.String(decisionAbstain)),
	)
	if err != nil {
		return nil, fmt.Errorf("build contract environment: %w", err)
	}

	engine := &Engine{contracts: make([]compiledContract, 0, len(g.Contracts))}
	for _, c := range g.Contracts {
		ast, issues := env.Compile(c.Expression)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("compile contract %s: %w", c.ID, issues.Err())
		}
		if !ast.OutputType().IsExactType(cel.StringType) {
			return nil, fmt.Errorf("contract %s must return a decision string", c.ID)
		}
		prg, err := env.Program(ast, cel.CostLimit(contractCostLimit))
		if err != nil {
			return nil, fmt.Errorf("program contract %s: %w", c.ID, err)
		}
		engine.contracts = append(engine.contracts, compiledContract{contract: c, program: prg, inputs: contractInputs(c.Expression)})
	}
	return engine, nil
}

func contractInputs(expr string) []string {
	var refs []string
	for _, m := range inputRefPattern.FindAllStringSubmatch(expr, -1) {
		path := m[1] + "." + m[2]
		if m[3] != "" {
			path += "." + m[3]
		}
		if !slices.Contains(refs, path) {
			refs = append(refs, path)
		}
	}
	return refs
}

// Evaluate runs every contract against the call and its labels and applies
// precedence: any BLOCK or contract error blocks, then any CLARIFY, then any
// PERMIT, else the default PERMIT.
func (e *Engine) Evaluate(action Action, labels LabelSet) (Decision, []string, []ContractOutcome) {
	vars := activation(action, labels)
	outcomes := make([]ContractOutcome, 0, len(e.contracts))
	for _, c := range e.contracts {
		outcome := ContractOutcome{ContractID: c.contract.ID, Result: "", Error: "", Inputs: resolveInputs(vars, c.inputs)}
		// CEL's && and || absorb errors, so a missing label must fail explicitly.
		missing := missingLabels(labels, c.inputs)
		var val ref.Val
		var err error
		if len(missing) == 0 {
			val, _, err = c.program.Eval(vars)
		}
		switch {
		case len(missing) > 0:
			outcome.Error = ErrorMissingLabel + strings.Join(missing, ", ")
		case err != nil:
			outcome.Error = err.Error()
		default:
			s, ok := val.Value().(string)
			d := Decision(s)
			if !ok || !slices.Contains([]Decision{DecisionPermit, DecisionClarify, DecisionBlock, decisionAbstain}, d) {
				outcome.Error = fmt.Sprintf("contract returned %v, not a decision", val.Value())
			} else {
				outcome.Result = d
			}
		}
		outcomes = append(outcomes, outcome)
	}
	decision, decidedBy := Combine(outcomes)
	return decision, decidedBy, outcomes
}

// Combine applies the fixed precedence over contract outcomes.
func Combine(outcomes []ContractOutcome) (Decision, []string) {
	pick := func(match func(ContractOutcome) bool) []string {
		var ids []string
		for _, o := range outcomes {
			if match(o) {
				ids = append(ids, o.ContractID)
			}
		}
		return ids
	}
	if ids := pick(func(o ContractOutcome) bool { return o.Error != "" || o.Result == DecisionBlock }); len(ids) > 0 {
		return DecisionBlock, ids
	}
	if ids := pick(func(o ContractOutcome) bool { return o.Result == DecisionClarify }); len(ids) > 0 {
		return DecisionClarify, ids
	}
	if ids := pick(func(o ContractOutcome) bool { return o.Result == DecisionPermit }); len(ids) > 0 {
		return DecisionPermit, ids
	}
	return DecisionPermit, nil
}

func activation(action Action, labels LabelSet) map[string]any {
	label := make(map[string]any, len(labels))
	rank := make(map[string]int64, len(labels))
	act := map[string]any{}
	for key, v := range labels {
		label[key] = v.Value
		rank[key] = int64(v.Rank)
		if key == LabelEffectClass {
			act["effect_class"] = v.Value
		}
	}
	return map[string]any{
		"label":  label,
		"rank":   rank,
		"action": act,
		"tool": map[string]any{
			"name":       action.Tool.Name,
			"mcp_server": action.Tool.MCPServer,
			"mcp_tool":   action.Tool.MCPFunction,
			"args":       parseArgs(action.Arguments),
		},
	}
}

// parseArgs exposes object arguments as a map. Anything else becomes an empty
// map so field tests on non-shell calls do not error.
func parseArgs(raw json.RawMessage) map[string]any {
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil || args == nil {
		return map[string]any{}
	}
	return args
}

// ErrorMissingLabel prefixes the outcome error of a contract that reads a label
// the evaluator did not answer.
const ErrorMissingLabel = "evaluator did not answer: "

func missingLabels(labels LabelSet, refs []string) []string {
	var missing []string
	for _, r := range refs {
		parts := strings.Split(r, ".")
		var key string
		switch parts[0] {
		case "label", "rank":
			key = parts[1]
		case "action":
			if parts[1] != "effect_class" {
				continue
			}
			key = LabelEffectClass
		default:
			continue
		}
		if _, ok := labels[key]; !ok && !slices.Contains(missing, key) {
			missing = append(missing, key)
		}
	}
	return missing
}

var errMissingInput = errors.New("missing")

func resolveInputs(vars map[string]any, refs []string) map[string]any {
	inputs := make(map[string]any, len(refs))
	for _, r := range refs {
		v, err := lookup(vars, strings.Split(r, "."))
		if err != nil {
			inputs[r] = nil
			continue
		}
		inputs[r] = v
	}
	return inputs
}

func lookup(v any, path []string) (any, error) {
	for _, part := range path {
		switch m := v.(type) {
		case map[string]any:
			next, ok := m[part]
			if !ok {
				return nil, errMissingInput
			}
			v = next
		case map[string]int64:
			next, ok := m[part]
			if !ok {
				return nil, errMissingInput
			}
			v = next
		default:
			return nil, errMissingInput
		}
	}
	return v, nil
}
