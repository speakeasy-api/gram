package risk_analysis

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"

	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

// This file is the CEL evaluation path for custom detection and policy scopes,
// backed by internal/risk/celenv. Rules store a CEL detection predicate
// (detection_cel, legacy regex evaluated as content.match(regex)); policies
// store CEL scope predicates (scope_include_cel / scope_exempt_cel).

// celEngine is the shared, immutable CEL environment, built once. Eval is
// thread-safe, so a single engine serves the batch analyzer and the realtime
// scanner concurrently.
var celEngine = sync.OnceValues(celenv.New)

// CELEngine returns the shared CEL engine.
func CELEngine() (*celenv.Engine, error) {
	return celEngine()
}

// celMessage adapts the structured MessageView into the celenv input model.
func celMessage(view MessageView) celenv.Message {
	tools := make([]celenv.Tool, len(view.Tools))
	for i, t := range view.Tools {
		tools[i] = celenv.Tool{Name: t.Name, Server: t.Server, Function: t.Function, Args: t.Arguments}
	}
	return celenv.Message{Content: view.Content, Type: view.Type, Tools: tools}
}

// effectiveDetectionCEL returns the CEL predicate a rule should evaluate: its
// detection_cel when set, else a synthesized content.match(regex) for a legacy
// regex rule, else empty (no matcher configured).
func effectiveDetectionCEL(rule CustomDetectionRule) string {
	if expr := strings.TrimSpace(rule.DetectionCel); expr != "" {
		return expr
	}
	if pattern := strings.TrimSpace(rule.Regex); pattern != "" {
		return "content.match(" + strconv.Quote(pattern) + ")"
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
		expr := effectiveDetectionCEL(rule)
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
// per recorded span. Custom rules are pure detectors; message exemptions are
// handled by the policy's scope_exempt_cel (CompiledScope.Exempts), not by rules.
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

// CompiledScope is a policy's compiled CEL applicability predicates. A nil
// include program means all-in; a nil exempt program means none-exempt.
type CompiledScope struct {
	eng     *celenv.Engine
	include cel.Program
	exempt  cel.Program
}

// CompileScope compiles a policy's scope predicates. Empty strings compile to nil
// programs (all-in / none-exempt).
func CompileScope(eng *celenv.Engine, includeCEL, exemptCEL string) (CompiledScope, error) {
	s := CompiledScope{eng: eng, include: nil, exempt: nil}
	if expr := strings.TrimSpace(includeCEL); expr != "" {
		prg, err := eng.Compile(expr)
		if err != nil {
			return CompiledScope{}, fmt.Errorf("compile scope_include_cel %q: %w", expr, err)
		}
		s.include = prg
	}
	if expr := strings.TrimSpace(exemptCEL); expr != "" {
		prg, err := eng.Compile(expr)
		if err != nil {
			return CompiledScope{}, fmt.Errorf("compile scope_exempt_cel %q: %w", expr, err)
		}
		s.exempt = prg
	}
	return s, nil
}

// HasIncludeScope reports whether a scope_include_cel value narrows scope (is
// non-empty), without compiling it — a cheap candidate pre-filter.
func HasIncludeScope(includeCEL string) bool { return strings.TrimSpace(includeCEL) != "" }

// HasInclude reports whether the scope narrows which messages are in scope (an
// include predicate is set). When false the policy falls back to message_types.
func (s CompiledScope) HasInclude() bool { return s.include != nil }

// Active reports whether the scope has any predicate to evaluate.
func (s CompiledScope) Active() bool { return s.include != nil || s.exempt != nil }

// Includes reports whether a message is in scope. A nil include means all-in.
// A post-compile eval error fails toward scanning (in-scope) so detection is
// never silently skipped.
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

// Exempts reports whether a message is exempted. A nil exempt means none-exempt.
// A post-compile eval error fails toward scanning (not exempt).
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
