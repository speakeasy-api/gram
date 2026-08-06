package customruleanalyzer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/scanners"
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
	findingsPub gcp.Publisher[*riskv1.Finding]
	scanner     *Scanner
}

// NewHandler builds a custom-rules subscription handler from a logger, the
// scanner that loads and evaluates rules (see NewScanner), and the publisher for
// the shared Finding topic.
func NewHandler(logger *slog.Logger, scanner *Scanner, findingsPub gcp.Publisher[*riskv1.Finding]) *Handler {
	return &Handler{
		logger:      logger.With(attr.SlogComponent("custom-rules-analyzer")),
		findingsPub: findingsPub,
		scanner:     scanner,
	}
}

// Handle scans the request against its selected custom rules and publishes one
// Finding per matched span. Errors are classified so one bad rule cannot poison
// the subscription while we run in shadow mode: a rule-load failure is transient
// and nacks the message so it redelivers, whereas a rule evaluation error (e.g.
// a malformed CEL predicate stored in the database) is permanent and is logged
// and swallowed. A malformed project id nacks — the publisher always sets a
// valid id, so it flags a corrupt message rather than a bad rule.
func (h *Handler) Handle(ctx context.Context, m *riskv1.CustomRulesAnalysis, _ gcp.MessageMetadata) error {
	projectID, err := uuid.Parse(m.GetProjectId())
	if err != nil {
		return fmt.Errorf("invalid project id: %w", err)
	}

	toolCalls := make([]ScanToolCall, 0, len(m.GetToolCalls()))
	for _, tc := range m.GetToolCalls() {
		toolCalls = append(toolCalls, ScanToolCall{Name: tc.GetName(), Arguments: tc.GetArguments()})
	}

	findings, err := h.scanner.Scan(ctx, ScanRequest{
		ProjectID:     projectID,
		CustomRuleIDs: m.GetCustomRuleIds(),
		Content:       m.GetContent(),
		Kind:          m.GetKind(),
		ToolCalls:     toolCalls,
	})
	var loadErr *loadError
	switch {
	case errors.As(err, &loadErr):
		// Transient: nack so the message redelivers once the database recovers.
		return fmt.Errorf("scan custom rules: %w", err)
	case err != nil:
		// Permanent (e.g. a bad rule predicate): swallow so it cannot poison the
		// subscription. The error names the offending rule id.
		h.logger.WarnContext(ctx, "custom rule scan failed, skipping message", attr.SlogError(err), attr.SlogMessageID(m.GetChatMessageId()))
		return nil
	}

	// Issue every publish first so the Pub/Sub client can batch them, then drain
	// the futures — mirrors the publish-then-drain pattern in the gitleaks
	// handler. StartPublishFindings mints deterministic ids — a redelivered
	// message republishes under the same ids instead of duplicating ClickHouse
	// rows — and stamps the reveal metadata (surface/field/path/tool_call_id)
	// from each finding's span attribution.
	results, ruleIDs := scanners.StartPublishFindings(ctx, h.findingsPub, scanners.FindingMetadata{
		RequestID:         m.GetRequestId(),
		ChatMessageID:     m.GetChatMessageId(),
		ContentPartID:     m.GetContentPartId(),
		ProjectID:         m.GetProjectId(),
		OrganizationID:    m.GetOrganizationId(),
		RiskPolicyID:      m.GetRiskPolicyId(),
		RiskPolicyVersion: m.GetRiskPolicyVersion(),
	}, findings)

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
		"matches":         len(findings),
		"published":       published,
		"rule_ids":        ruleIDs,
	}))

	return nil
}
