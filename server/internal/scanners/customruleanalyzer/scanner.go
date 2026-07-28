package customruleanalyzer

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/customrules"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// ScanRequest is the transport-agnostic input Scan needs: the project whose
// rules to load, which of them to evaluate, and the message (content, kind,
// tool calls) to evaluate against. Server/Function are derived from each tool
// call's Name inside Scan, matching NewToolView.
type ScanRequest struct {
	ProjectID     uuid.UUID
	CustomRuleIDs []string
	Content       string
	Kind          string
	ToolCalls     []ScanToolCall
}

// ScanToolCall is one tool invocation carried in the message.
type ScanToolCall struct {
	Name      string
	Arguments string
}

// ScanBatchRequest evaluates the same selected rule set against many messages
// belonging to one project. The rules are loaded from the database once for the
// whole batch, so DB work stays constant in the number of messages.
type ScanBatchRequest struct {
	ProjectID     uuid.UUID
	CustomRuleIDs []string
	Messages      []ScanMessage
}

// ScanMessage is a single message within a batch: the CEL input to evaluate.
type ScanMessage struct {
	Content   string
	Kind      string
	ToolCalls []ScanToolCall
}

type Scanner struct {
	db   repo.DBTX
	eval *evaluator
}

func NewScanner(db repo.DBTX) (*Scanner, error) {
	eval, err := newEvaluator(evaluatorCacheSize)
	if err != nil {
		return nil, err
	}

	return &Scanner{db: db, eval: eval}, nil
}

func (s *Scanner) Scan(ctx context.Context, req ScanRequest) ([]scanners.Finding, error) {
	rules, err := customrules.LoadSelected(ctx, repo.New(s.db), req.ProjectID, req.CustomRuleIDs)
	if err != nil {
		return nil, &loadError{err: fmt.Errorf("load custom detection rules: %w", err)}
	}

	if len(rules) == 0 {
		return []scanners.Finding{}, nil
	}

	return s.evaluate(rules, celMessageFromMessage(ScanMessage{
		Content:   req.Content,
		Kind:      req.Kind,
		ToolCalls: req.ToolCalls,
	}))
}

// ScanBatch evaluates the selected rule set against every message in req,
// loading the rules from the database a single time. The returned slice is
// index-aligned with req.Messages.
func (s *Scanner) ScanBatch(ctx context.Context, req ScanBatchRequest) ([][]scanners.Finding, error) {
	out := make([][]scanners.Finding, len(req.Messages))
	for i := range out {
		out[i] = []scanners.Finding{}
	}

	// Nothing to scan: skip the rule load entirely so an empty batch can't fail
	// on a transient DB error (which would nack and redeliver a no-op message).
	if len(req.Messages) == 0 {
		return out, nil
	}

	rules, err := customrules.LoadSelected(ctx, repo.New(s.db), req.ProjectID, req.CustomRuleIDs)
	if err != nil {
		return nil, &loadError{err: fmt.Errorf("load custom detection rules: %w", err)}
	}

	if len(rules) == 0 {
		return out, nil
	}

	for i, m := range req.Messages {
		findings, err := s.evaluate(rules, celMessageFromMessage(m))
		if err != nil {
			return nil, err
		}
		out[i] = findings
	}

	return out, nil
}

// evaluate runs each rule's detection expression against a single CEL message
// and collects the matched spans as findings. It is shared by Scan and
// ScanBatch so both produce identical findings; only rule loading differs.
func (s *Scanner) evaluate(rules []customrules.Rule, msg celenv.Message) ([]scanners.Finding, error) {
	findings := []scanners.Finding{}
	for _, rule := range rules {
		expr := rule.EffectiveDetectionExpr()
		if expr == "" {
			continue
		}

		spans, matched, err := s.eval.execute(expr, msg)
		if err != nil {
			return nil, fmt.Errorf("evaluate custom rule %q: %w", rule.RuleID, err)
		}

		if !matched {
			continue
		}

		for _, s := range spans {
			findings = append(findings, scanners.Finding{
				RuleID:       scanners.GuardRuleID(rule.RuleID),
				Description:  rule.DisplayDescription(),
				Match:        s.Value,
				StartPos:     s.Start,
				EndPos:       s.End,
				Tags:         []string{},
				Source:       Source,
				Confidence:   1.0,
				SpanGroupKey: s.ToolCallID,
				Field:        s.Target,
				Path:         s.Path,

				DeadLetterReason:    "",
				McpLookupToolCallID: "",
			})
		}
	}

	return findings, nil
}

// celMessageFromMessage builds the CEL input model from a single scan message.
// Server/function are derived from each tool name the same way NewToolView does
// in the risk_analysis activity, keeping async and in-process evaluation aligned.
func celMessageFromMessage(m ScanMessage) celenv.Message {
	tools := make([]celenv.Tool, 0, len(m.ToolCalls))
	for _, tc := range m.ToolCalls {
		tools = append(tools, celenv.Tool{
			Name:     tc.Name,
			Server:   toolref.MCPServerOf(tc.Name),
			Function: toolref.MCPFunctionOf(tc.Name),
			Args:     tc.Arguments,
		})
	}

	return celenv.Message{
		Content: m.Content,
		Type:    m.Kind,
		Tools:   tools,
	}
}

// loadError indicates the scanner could not load the selected rules from the
// database. Unlike a rule evaluation error — which is caused by a bad rule and
// will fail identically on every retry — a load failure is transient, so a
// caller draining an at-least-once subscription should nack and let the message
// redeliver rather than drop it.
type loadError struct{ err error }

func (e *loadError) Error() string { return e.err.Error() }
func (e *loadError) Unwrap() error { return e.err }
