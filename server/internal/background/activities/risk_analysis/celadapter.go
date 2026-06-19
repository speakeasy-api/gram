package risk_analysis

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/cel-go/cel"

	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

// This file bridges the structured custom-detection engine to the CEL engine in
// internal/risk/celenv. It does three things:
//
//   - celMessage adapts a MessageView into a celenv.Message.
//   - MatchConfigToCEL serializes a structured match_config into an equivalent
//     CEL predicate, so today's stored rules can run through celenv without any
//     storage or API change. Tool conditions are grouped into a single
//     tools.exists(t, ...) so they are CORRELATED to the same tool call (the
//     intended behavior; the legacy structured engine matched tool conditions
//     independently across calls).
//   - ScanCELRules evaluates compiled CEL rules over a view and converts the
//     recorded spans into Findings (one Finding per span, preserving the
//     single-Match Finding shape; multi-span rules emit multiple Findings).
//
// The live custom-rule path (ScanCustomDetectionRules) is unchanged; activating
// CEL is a one-line swap at the call site once the surrounding migration lands.

// celMessage adapts the structured MessageView into the celenv input model.
func celMessage(view MessageView) celenv.Message {
	tools := make([]celenv.Tool, len(view.Tools))
	for i, t := range view.Tools {
		tools[i] = celenv.Tool{Name: t.Name, Server: t.Server, Function: t.Function, Args: t.Arguments}
	}
	return celenv.Message{Content: view.Content, Type: view.Type, Tools: tools}
}

// CompiledCELRule is a custom rule whose match_config has been serialized to CEL
// and compiled once for repeated evaluation.
type CompiledCELRule struct {
	rule CustomDetectionRule
	prg  cel.Program
}

// CompileCELRules serializes each rule's match_config to CEL and compiles it.
func CompileCELRules(eng *celenv.Engine, rules []CustomDetectionRule) ([]CompiledCELRule, error) {
	out := make([]CompiledCELRule, 0, len(rules))
	for _, rule := range rules {
		if isEmptyJSON(rule.MatchConfig) {
			continue
		}
		cfg, err := parseMatchConfig(rule.MatchConfig)
		if err != nil {
			return nil, fmt.Errorf("custom rule %s: %w", rule.RuleID, err)
		}
		if len(cfg.Conditions) == 0 {
			continue
		}
		expr, err := MatchConfigToCEL(cfg)
		if err != nil {
			return nil, fmt.Errorf("custom rule %s: serialize match_config: %w", rule.RuleID, err)
		}
		prg, err := eng.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("custom rule %s: compile %q: %w", rule.RuleID, expr, err)
		}
		out = append(out, CompiledCELRule{rule: rule, prg: prg})
	}
	return out, nil
}

// ScanCELRules evaluates the deny rules over a view, returning one Finding per
// recorded span. (Allow/exempt rules are handled by the caller as today.)
func ScanCELRules(eng *celenv.Engine, view MessageView, rules []CompiledCELRule) ([]Finding, error) {
	msg := celMessage(view)
	var findings []Finding
	for _, r := range rules {
		spans, matched, err := eng.EvalDetection(r.prg, msg)
		if err != nil {
			return nil, fmt.Errorf("custom rule %s: eval: %w", r.rule.RuleID, err)
		}
		if !matched {
			continue
		}
		for _, s := range spans {
			findings = append(findings, Finding{
				RuleID:           r.rule.RuleID,
				Description:      celRuleDescription(r.rule),
				Match:            s.Value,
				StartPos:         s.Start,
				EndPos:           s.End,
				Tags:             nil,
				Source:           SourceCustom,
				Confidence:       1.0,
				DeadLetterReason: "",
				toolCallID:       s.ToolCallID,
			})
		}
	}
	return findings, nil
}

func celRuleDescription(rule CustomDetectionRule) string {
	if strings.TrimSpace(rule.Description) != "" {
		return rule.Description
	}
	if strings.TrimSpace(rule.Title) != "" {
		return rule.Title
	}
	return rule.RuleID
}

// MatchConfigToCEL serializes a structured match_config into a boolean CEL
// predicate over the celenv environment.
func MatchConfigToCEL(cfg MatchConfig) (string, error) {
	op := " && "
	if cfg.combineOrDefault() == CombineOr {
		op = " || "
	}

	var nonTool, toolConds []string
	for _, c := range cfg.Conditions {
		expr, isTool, err := conditionToCEL(c)
		if err != nil {
			return "", err
		}
		if isTool {
			toolConds = append(toolConds, expr)
		} else {
			nonTool = append(nonTool, expr)
		}
	}

	parts := nonTool
	if len(toolConds) > 0 {
		parts = append(parts, "tools.exists(t, "+strings.Join(toolConds, op)+")")
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("match_config has no conditions")
	}
	return strings.Join(parts, op), nil
}

// conditionToCEL renders one condition, reporting whether it reads a tool field
// (and so must live inside tools.exists).
func conditionToCEL(c Condition) (string, bool, error) {
	field, isTool, err := targetField(c.Target)
	if err != nil {
		return "", false, err
	}
	if c.Target == TargetToolArgs && strings.TrimSpace(c.Path) != "" {
		field += ".get(" + quote(c.Path) + ")"
	}
	expr, err := opToCEL(field, c)
	if err != nil {
		return "", false, err
	}
	return expr, isTool, nil
}

func targetField(t Target) (field string, isTool bool, err error) {
	switch t {
	case TargetContent:
		return "content", false, nil
	case TargetUserPrompt:
		return "prompt", false, nil
	case TargetAssistant:
		return "assistant", false, nil
	case TargetToolResult:
		return "output", false, nil
	case TargetToolName:
		return "t.name", true, nil
	case TargetToolServer:
		return "t.server", true, nil
	case TargetToolFunction:
		return "t.function", true, nil
	case TargetToolArgs:
		return "t.args", true, nil
	default:
		return "", false, fmt.Errorf("unsupported target %q", t)
	}
}

func opToCEL(field string, c Condition) (string, error) {
	switch c.Op {
	case OpRegex:
		return field + ".match(" + quote(c.Value) + ")", nil
	case OpEquals:
		return field + ".eq(" + quote(c.Value) + ")", nil
	case OpNotEquals:
		return "!(" + field + ".eq(" + quote(c.Value) + "))", nil
	case OpGlob:
		return field + ".glob(" + quote(c.Value) + ")", nil
	case OpExists:
		return field + ".present()", nil
	case OpStartsWith:
		return field + ".prefix(" + quote(c.Value) + ")", nil
	case OpEndsWith:
		return field + ".suffix(" + quote(c.Value) + ")", nil
	case OpKeyword, OpContains:
		return orJoin(field, "includes", operandsOf(c)), nil
	case OpNotContains:
		return "!(" + orJoin(field, "includes", operandsOf(c)) + ")", nil
	case OpIn:
		return orJoin(field, "eq", operandsOf(c)), nil
	default:
		return "", fmt.Errorf("unsupported op %q", c.Op)
	}
}

// operandsOf returns the union-capable operands for contains/not_contains/in/
// keyword: Values when present, else the single Value.
func operandsOf(c Condition) []string {
	if len(c.Values) > 0 {
		return c.Values
	}
	return []string{c.Value}
}

// orJoin renders `(field.method(o1) || field.method(o2) || ...)`.
func orJoin(field, method string, operands []string) string {
	terms := make([]string, len(operands))
	for i, o := range operands {
		terms[i] = field + "." + method + "(" + quote(o) + ")"
	}
	if len(terms) == 1 {
		return terms[0]
	}
	return "(" + strings.Join(terms, " || ") + ")"
}

// quote renders a Go/CEL-compatible double-quoted string literal.
func quote(s string) string {
	return strconv.Quote(s)
}
