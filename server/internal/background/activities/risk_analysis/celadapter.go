package risk_analysis

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/cel-go/cel"

	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

// This file is the CEL evaluation path for custom detection rules, backed by
// internal/risk/celenv. Rules store a CEL detection predicate (detection_expr,
// legacy regex evaluated as content.matchRegex(regex)).
//
// The CEL engine is immutable and Eval is thread-safe, so a single instance is
// constructed once at each composition root (server start, worker activities)
// and injected into the consumers (Scanner, Service, AnalyzeBatch) rather than
// reached through a package global.

// celMessage adapts the structured MessageView into the celenv input model.
func celMessage(view MessageView) celenv.Message {
	tools := make([]celenv.Tool, len(view.Tools))
	for i, t := range view.Tools {
		tools[i] = celenv.Tool{Name: t.Name, Server: t.Server, Function: t.Function, Args: t.Arguments}
	}
	return celenv.Message{Content: view.Content, Type: view.Type, Tools: tools}
}

// effectiveDetectionExpr returns the CEL predicate a rule should evaluate: its
// detection_expr when set, else a synthesized content.matchRegex(regex) for a legacy
// regex rule, else empty (no matcher configured).
func effectiveDetectionExpr(rule CustomDetectionRule) string {
	if expr := strings.TrimSpace(rule.DetectionExpr); expr != "" {
		return expr
	}
	if pattern := strings.TrimSpace(rule.Regex); pattern != "" {
		return "content.matchRegex(" + strconv.Quote(pattern) + ")"
	}
	return ""
}

// CompiledCELRule is a custom rule whose detection predicate is compiled once.
type CompiledCELRule struct {
	rule CustomDetectionRule
	prg  cel.Program
}

// CompileCELRules compiles each rule's effective detection predicate. Rules
// without a matcher are skipped.
func CompileCELRules(eng *celenv.Engine, rules []CustomDetectionRule) ([]CompiledCELRule, error) {
	out := make([]CompiledCELRule, 0, len(rules))
	for _, rule := range rules {
		expr := effectiveDetectionExpr(rule)
		if expr == "" {
			continue
		}
		prg, err := eng.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("custom rule %s: compile %q: %w", rule.RuleID, expr, err)
		}
		rule.RuleID = guard(rule.RuleID)
		out = append(out, CompiledCELRule{rule: rule, prg: prg})
	}
	return out, nil
}

// ScanCELRules evaluates the (detector) rules over a view, producing one Finding
// per recorded span. Custom rules are pure detectors.
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
				field:            s.Target,
				path:             s.Path,
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
