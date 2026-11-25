package risk_analysis

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk/policyflags"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

const (
	// shadowStreamExecutionPath labels the per-source analysis handlers' scans
	// (gitleaks, presidio, custom rules, tool calls) on metering readings and
	// spans. The value is kept stable for existing metric queries.
	shadowStreamExecutionPath = "shadow_stream"

	// judgeHandlerStreamExecutionPath labels the prompt-injection and
	// prompt-policy handlers' judge scans.
	judgeHandlerStreamExecutionPath = "judge_handler_stream"

	// llmAnalyzerStreamExecutionPath marks analysis requests the fine-tuned
	// LLM analyzer's streams consumer evaluates as the only engine for the
	// policy's sources.
	llmAnalyzerStreamExecutionPath = "llm_analyzer_stream"

	// llmShadowStreamExecutionPath marks analysis requests the fine-tuned LLM
	// analyzer evaluates in the shadow engine mode: the per-source handlers
	// scan the same batch and enforce, and the model's verdict is recorded for
	// comparison only.
	llmShadowStreamExecutionPath = "llm_shadow_stream"
)

// batchOperationID identifies metering for one policy execution and scanned
// anchor independently of transport correlation and batch composition.
func batchOperationID(args AnalyzeBatchArgs, msg batchMessage, executionPath string) string {
	chatMessageID, contentPartID := msg.anchorIDStrings()
	return scanners.AsyncRiskOperationID(
		executionPath,
		args.RiskPolicyID.String(),
		args.PolicyVersion,
		derefOrEmpty(chatMessageID),
		derefOrEmpty(contentPartID),
		"",
	)
}

// scanLane separates a message's standard-policy and prompt-policy scan
// requests so their request ids never collide.
type scanLane string

const (
	scanLaneStandard     scanLane = "standard"
	scanLanePromptPolicy scanLane = "prompt_policy"
)

// batchScanRequestID derives the correlation id of one anchor's scan request
// from the anchor, policy, version and standard/prompt-policy lane, never from
// the rest of the batch. The handlers fold it into their finding ids, so a
// retry that lands the anchor in a differently composed batch republishes the
// same finding ids and ClickHouse converges instead of duplicating.
func batchScanRequestID(args AnalyzeBatchArgs, lane scanLane, contentPart bool, anchorID uuid.UUID) uuid.UUID {
	anchorKind := "chat_message"
	if contentPart {
		anchorKind = "content_part"
	}
	parts := []string{
		string(lane),
		args.ProjectID.String(),
		args.RiskPolicyID.String(),
		strconv.FormatInt(args.PolicyVersion, 10),
		anchorKind,
		anchorID.String(),
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:scanrequest:anchor:"+strings.Join(parts, "\x00")))
}

// messageScanRequestID is batchScanRequestID for a scanned message or part.
func messageScanRequestID(args AnalyzeBatchArgs, lane scanLane, msg batchMessage) string {
	return batchScanRequestID(args, lane, msg.ContentPart, msg.ID).String()
}

func nilUUIDString(id uuid.UUID) *string {
	if id == uuid.Nil {
		return nil
	}
	value := id.String()
	return &value
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func batchRiskProvenance(args AnalyzeBatchArgs, msg batchMessage, executionPath, requestID string) metering.RiskProvenance {
	chatMessageID := msg.ID
	messageLinkReason := ""
	if msg.ContentPart {
		chatMessageID = msg.ParentChatMessageID
		if chatMessageID == uuid.Nil {
			messageLinkReason = "content_part_unlinked"
		}
	}

	toolCallID := ""
	toolName := ""
	if len(msg.ToolCalls) == 1 {
		toolCallID = msg.ToolCalls[0].ID
		toolName = msg.ToolCalls[0].Function.Name
	}

	return metering.RiskProvenance{
		OrganizationID:         args.OrganizationID,
		ProjectID:              args.ProjectID,
		RiskPolicyID:           args.RiskPolicyID,
		RiskPolicyVersion:      args.PolicyVersion,
		PolicyLinkReason:       "",
		ChatID:                 msg.ChatID,
		ExternalConversationID: "",
		ChatMessageID:          chatMessageID,
		ContentPartID:          msg.chatContentPartID().UUID,
		MessageLinkReason:      messageLinkReason,
		OperationID:            batchOperationID(args, msg, executionPath),
		ExecutionPath:          executionPath,
		RequestID:              requestID,
		MessageType:            msg.Type,
		HookSource:             msg.Source,
		UserID:                 msg.UserID,
		ToolCallID:             toolCallID,
		ToolName:               toolName,
		Model:                  "",
		Provider:               "",
	}
}

// llmLane is the fine-tuned LLM analyzer lane a standard-policy batch uses.
type llmLane struct {
	// enforcing means the lane replaces the per-source engines for the sources the
	// model enforces (the llm risk engine mode).
	enforcing bool

	// shadow means the lane only records the model's verdict for comparison
	// while the per-source engines keep enforcing (the shadow risk engine mode).
	shadow bool

	// orgSlug is carried on each request for the analyzer's attribution.
	orgSlug string
}

// ownEngineRuns reports whether source's own engine still evaluates it.
// prompt_injection is only compared by the model, so its engine always runs.
func (l llmLane) ownEngineRuns(source string) bool {
	return !l.enforcing || !llmanalyzer.Enforces(source)
}

// runs reports whether the policy selects source and its own engine evaluates it.
func (l llmLane) runs(sources sourceSet, source string) bool {
	return sources.Has(source) && l.ownEngineRuns(source)
}

// resolveLLMLane reads the risk engine mode for the batch's organization.
//
// The mode selects the engine behind every covered source (gitleaks,
// presidio, prompt_injection, destructive_tool, cli_destructive). In the llm
// mode the fine-tuned model's async lane replaces the per-source engines: no
// per-source analysis request for those sources, whose findings come from the
// model alone. In the shadow mode the per-source engines run exactly as in the
// off mode and the same messages are also published to the model's lane,
// marked shadow, so both engines' verdicts can be compared on identical
// traffic without the model ever enforcing. Custom rules, shadow_mcp and
// account_identity keep their engines in every mode.
//
// Both model modes only publish when this worker knows an analyzer is
// configured: with GRAM_RISK_LLM_URL empty the streams consumer acks every
// request without findings. The llm mode then falls back to the per-source
// engines rather than leave the organization without async coverage; the
// shadow mode just skips the comparison lane.
func (a *AnalyzeBatch) resolveLLMLane(ctx context.Context, args AnalyzeBatchArgs) llmLane {
	lane := llmLane{enforcing: false, shadow: false, orgSlug: ""}
	if !llmanalyzer.CoversAnySource(args.Sources) {
		return lane
	}
	mode, orgSlug := policyflags.ProjectFlagMode(ctx, a.logger, repo.New(a.db), a.flags, args.OrganizationID, args.ProjectID, feature.FlagRiskLLMAnalyzer)
	switch {
	case mode == feature.VariantRiskLLMLLM && a.llmAnalyzerEnabled:
		lane.enforcing, lane.orgSlug = true, orgSlug
	case mode == feature.VariantRiskLLMLLM:
		a.llmFallbackOnce.Do(func() {
			a.logger.WarnContext(ctx, "LLM analyzer mode llm but GRAM_RISK_LLM_URL empty; batch scans fall back to per-source engines",
				attr.SlogOrganizationID(args.OrganizationID),
				attr.SlogRiskPolicyID(args.RiskPolicyID.String()),
			)
		})
		a.metrics.RecordLLMPolicyEvaluation(ctx, args.OrganizationID, args.RiskPolicyID.String(), llmPolicyEvaluationFallbackLegacy, 1)
	case mode == feature.VariantRiskLLMShadow && a.llmAnalyzerEnabled:
		lane.shadow, lane.orgSlug = true, orgSlug
	case mode == feature.VariantRiskLLMShadow:
		a.metrics.RecordLLMPolicyEvaluation(ctx, args.OrganizationID, args.RiskPolicyID.String(), llmPolicyEvaluationShadowSkipped, 1)
	}
	return lane
}

// publishLLMLane publishes the batch to the model's lane when one is active.
// A shadow-lane publish failure is counted and swallowed: the shadow lane only
// compares, so a dropped request costs the comparison one message, while
// failing the activity would redrive the per-source engines behind an LLM
// transport outage. The enforcing lane fails the activity instead.
func (a *AnalyzeBatch) publishLLMLane(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, lane llmLane, sources sourceSet, masks CategoryScopeMasks) error {
	if !lane.enforcing && !lane.shadow {
		return nil
	}
	failed, err := a.publishLLMScanRequests(ctx, args, messages, lane.orgSlug, llmCoveredSources(sources), masks, lane.shadow)
	if err != nil && lane.shadow {
		a.logger.WarnContext(ctx, "shadow LLM analyzer scan dispatch failed for some messages; continuing without them",
			attr.SlogError(err),
			attr.SlogOrganizationID(args.OrganizationID),
			attr.SlogRiskPolicyID(args.RiskPolicyID.String()),
			attr.SlogRiskLLMPublishFailedCount(failed),
		)
		a.metrics.RecordLLMPolicyEvaluation(ctx, args.OrganizationID, args.RiskPolicyID.String(), llmPolicyEvaluationShadowPublishError, failed)
		return nil
	}
	if err != nil {
		return fmt.Errorf("llm analyzer scan dispatch: %w", err)
	}
	return nil
}
