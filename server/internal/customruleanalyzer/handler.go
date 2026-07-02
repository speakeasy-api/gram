package customruleanalyzer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/customrules"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// Source is the detection source stamped on every Finding this analyzer emits.
// It matches the in-process custom-rule scan's source so downstream consumers
// treat async and in-process custom findings identically.
const Source = "custom"

// Handler consumes CustomRulesAnalysis scan requests, loads the project's
// selected custom (CEL) detection rules from the read replica, evaluates them
// over the message, and publishes a Finding per match into the shared Finding
// topic. It is the async counterpart to the in-process custom-rule scan in the
// risk_analysis activity: nothing consumes the findings differently yet — the
// flow exercises the async pipeline end to end, like the gitleaks analyzer.
type Handler struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	findingsPub gcp.Publisher[*riskv1.Finding]
	eval        *evaluator
}

// NewHandler builds a custom-rules subscription handler. It creates the caching
// evaluator that compiles and memoizes rule predicates across all messages and
// projects (see evaluator.go). db must be a read replica pool.
func NewHandler(logger *slog.Logger, db *pgxpool.Pool, findingsPub gcp.Publisher[*riskv1.Finding]) (*Handler, error) {
	eval, err := newEvaluator(evaluatorCacheSize)
	if err != nil {
		return nil, err
	}

	return &Handler{
		logger:      logger.With(attr.SlogComponent("custom-rules-analyzer")),
		db:          db,
		findingsPub: findingsPub,
		eval:        eval,
	}, nil
}

// Handle scans the request against its selected custom rules and publishes one
// Finding per matched span. A failure to load rules nacks the message (it is
// transient); all other errors are logged and swallowed so a single bad message
// or rule cannot poison the subscription while we run in shadow mode.
func (h *Handler) Handle(ctx context.Context, m *riskv1.CustomRulesAnalysis, _ gcp.MessageMetadata) error {
	projectID, err := uuid.Parse(m.GetProjectId())
	if err != nil {
		// A malformed project id can never be processed: drop it rather than
		// redeliver forever.
		h.logger.WarnContext(ctx, "invalid project id on custom rules analysis", attr.SlogError(err))
		return nil
	}

	rules, err := customrules.LoadSelected(ctx, repo.New(h.db), projectID, m.GetCustomRuleIds())
	if err != nil {
		h.logger.ErrorContext(ctx, "load custom detection rules", attr.SlogError(err))
		return fmt.Errorf("load custom detection rules: %w", err)
	}
	if len(rules) == 0 {
		return nil
	}

	msg := celMessageFromProto(m)

	// One timestamp for the batch: when the findings were detected, distinct from
	// the request's created_at.
	createdAt := time.Now().UTC().Format(time.RFC3339)

	// Issue every publish first so the Pub/Sub client can batch them, then drain
	// the futures — mirrors the publish-then-drain pattern in the gitleaks handler.
	results := make([]gcp.PublishResult, 0)
	ruleIDs := make([]string, 0)
	for _, rule := range rules {
		expr := rule.EffectiveDetectionExpr()
		if expr == "" {
			continue
		}

		spans, matched, err := h.eval.execute(expr, msg)
		if err != nil {
			h.logger.WarnContext(ctx, "evaluate custom rule", attr.SlogError(err), attr.SlogRiskRuleID(rule.RuleID))
			continue
		}
		if !matched {
			continue
		}

		description := rule.DisplayDescription()
		for _, s := range spans {
			id, err := uuid.NewV7()
			if err != nil {
				h.logger.WarnContext(ctx, "failed to generate finding id", attr.SlogError(err))
				continue
			}

			startPos := conv.SafeInt32(s.Start)
			endPos := conv.SafeInt32(s.End)
			confidence := 1.0
			finding := riskv1.Finding_builder{
				Id:                new(id.String()),
				RequestId:         new(m.GetRequestId()),
				ChatMessageId:     new(m.GetChatMessageId()),
				ProjectId:         new(m.GetProjectId()),
				OrganizationId:    new(m.GetOrganizationId()),
				RiskPolicyId:      new(m.GetRiskPolicyId()),
				RiskPolicyVersion: new(m.GetRiskPolicyVersion()),
				CreatedAt:         &createdAt,
				RuleId:            new(rule.RuleID),
				Description:       &description,
				Match:             new(s.Value),
				StartPos:          &startPos,
				EndPos:            &endPos,
				Tags:              []string{},
				Source:            new(Source),
				Confidence:        &confidence,
			}.Build()

			results = append(results, h.findingsPub.Publish(ctx, finding))
			ruleIDs = append(ruleIDs, rule.RuleID)
		}
	}

	published := 0
	for _, res := range results {
		if _, err := res.Get(ctx); err != nil {
			h.logger.WarnContext(ctx, "failed to publish custom rule finding", attr.SlogError(err))
			continue
		}
		published++
	}

	// Never log matched values — they may carry sensitive data. Counts and rule
	// ids only.
	h.logger.InfoContext(ctx, "custom rules scan complete", attr.SlogValueAny(map[string]any{
		"request_id":      m.GetRequestId(),
		"chat_message_id": m.GetChatMessageId(),
		"matches":         len(results),
		"published":       published,
		"rule_ids":        ruleIDs,
	}))

	return nil
}

// celMessageFromProto rebuilds the CEL input model from the analysis request.
// Server/function are derived from each tool name the same way NewToolView does
// in the risk_analysis activity, keeping async and in-process evaluation aligned.
func celMessageFromProto(m *riskv1.CustomRulesAnalysis) celenv.Message {
	toolCalls := m.GetToolCalls()
	tools := make([]celenv.Tool, 0, len(toolCalls))
	for _, tc := range toolCalls {
		name := tc.GetName()
		tools = append(tools, celenv.Tool{
			Name:     name,
			Server:   toolref.MCPServerOf(name),
			Function: toolref.MCPFunctionOf(name),
			Args:     tc.GetArguments(),
		})
	}

	return celenv.Message{
		Content: m.GetContent(),
		Type:    m.GetKind(),
		Tools:   tools,
	}
}
