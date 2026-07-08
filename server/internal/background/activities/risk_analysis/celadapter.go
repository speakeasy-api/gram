package risk_analysis

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"

	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/customrules"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// celMessage adapts the structured MessageView into the celenv input model.
func celMessage(view MessageView) celenv.Message {
	tools := make([]celenv.Tool, len(view.Tools))
	for i, t := range view.Tools {
		tools[i] = celenv.Tool{Name: t.Name, Server: t.Server, Function: t.Function, Args: t.Arguments}
	}
	return celenv.Message{Content: view.Content, Type: view.Type, Tools: tools}
}

// CompiledCELRule is a custom rule whose detection predicate is compiled once.
type CompiledCELRule struct {
	rule customrules.Rule
	prg  cel.Program
}

// CompileCELRules compiles each rule's detection predicate.
func CompileCELRules(eng *celenv.Engine, rules []customrules.Rule) ([]CompiledCELRule, error) {
	if eng == nil {
		return []CompiledCELRule{}, nil
	}
	out := make([]CompiledCELRule, 0, len(rules))
	for _, rule := range rules {
		expr := rule.EffectiveDetectionExpr()
		if expr == "" {
			continue
		}
		prg, err := eng.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("custom rule %s: compile %q: %w", rule.RuleID, expr, err)
		}
		rule.RuleID = scanners.GuardRuleID(rule.RuleID)
		out = append(out, CompiledCELRule{rule: rule, prg: prg})
	}
	return out, nil
}

// ScanCELRules evaluates custom detector rules over a message view.
func ScanCELRules(eng *celenv.Engine, view MessageView, rules []CompiledCELRule) ([]scanners.Finding, error) {
	msg := celMessage(view)
	findings := []scanners.Finding{}
	for _, r := range rules {
		spans, matched, err := eng.EvalDetection(r.prg, msg)
		if err != nil {
			return []scanners.Finding{}, fmt.Errorf("custom rule %s: eval: %w", r.rule.RuleID, err)
		}
		if !matched {
			continue
		}
		for _, s := range spans {
			findings = append(findings, scanners.Finding{
				RuleID:              r.rule.RuleID,
				Description:         r.rule.DisplayDescription(),
				Match:               s.Value,
				StartPos:            s.Start,
				EndPos:              s.End,
				Tags:                []string{},
				Source:              SourceCustom,
				Confidence:          1.0,
				DeadLetterReason:    "",
				McpLookupToolCallID: "",
				SpanGroupKey:        s.ToolCallID,
				Field:               s.Target,
				Path:                s.Path,
			})
		}
	}
	return findings, nil
}

// CompiledScope is a policy's compiled scope predicates.
type CompiledScope struct {
	eng     *celenv.Engine
	include cel.Program
	exempt  cel.Program
}

// CompileScope compiles a policy's scope predicates.
func CompileScope(eng *celenv.Engine, includeCEL, exemptCEL string) (CompiledScope, error) {
	var s CompiledScope
	s.eng = eng
	includeCEL = strings.TrimSpace(includeCEL)
	exemptCEL = strings.TrimSpace(exemptCEL)
	if eng == nil {
		if includeCEL != "" || exemptCEL != "" {
			return CompiledScope{}, fmt.Errorf("cel engine unavailable")
		}
		return s, nil
	}
	if expr := includeCEL; expr != "" {
		prg, err := eng.Compile(expr)
		if err != nil {
			return CompiledScope{}, fmt.Errorf("compile scope_include %q: %w", expr, err)
		}
		s.include = prg
	}
	if expr := exemptCEL; expr != "" {
		prg, err := eng.Compile(expr)
		if err != nil {
			return CompiledScope{}, fmt.Errorf("compile scope_exempt %q: %w", expr, err)
		}
		s.exempt = prg
	}
	return s, nil
}

// Active reports whether the scope has any predicate to evaluate.
func (s CompiledScope) Active() bool { return s.include != nil || s.exempt != nil }

// InScope reports whether a message passes include and does not match exempt.
func (s CompiledScope) InScope(view MessageView) (bool, error) {
	msg := celMessage(view)
	if s.include != nil {
		in, err := s.eng.EvalScope(s.include, msg)
		if err != nil {
			return false, fmt.Errorf("eval scope_include: %w", err)
		}
		if !in {
			return false, nil
		}
	}
	if s.exempt != nil {
		ex, err := s.eng.EvalScope(s.exempt, msg)
		if err != nil {
			return false, fmt.Errorf("eval scope_exempt: %w", err)
		}
		if ex {
			return false, nil
		}
	}
	return true, nil
}

// Includes reports whether a message is in scope.
func (s CompiledScope) Includes(view MessageView) bool {
	if s.include == nil {
		return true
	}
	in, err := s.eng.EvalScope(s.include, celMessage(view))
	if err != nil {
		return true
	}
	return in
}

// Exempts reports whether a message is exempted.
func (s CompiledScope) Exempts(view MessageView) bool {
	if s.exempt == nil {
		return false
	}
	ex, err := s.eng.EvalScope(s.exempt, celMessage(view))
	if err != nil {
		return false
	}
	return ex
}
