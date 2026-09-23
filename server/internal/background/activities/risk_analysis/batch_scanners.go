package risk_analysis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk/policyflags"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

const (
	inlineBatchExecutionPath  = "inline_batch"
	shadowStreamExecutionPath = "shadow_stream"
	// promptInjectionStreamExecutionPath marks prompt injection analysis
	// requests the streams consumer evaluates as the only engine for the
	// batch. It replaces shadowStreamExecutionPath on this source: nothing
	// scans prompt injection inline any more, so the requests are not a
	// shadow of an inline scan and their metering is the batch's only
	// prompt-injection usage (AIS-722).
	promptInjectionStreamExecutionPath = "prompt_injection_stream"
	// llmAnalyzerStreamExecutionPath marks analysis requests the fine-tuned
	// LLM analyzer's streams consumer evaluates as the only engine for the
	// policy's sources, as opposed to the shadow stream that runs alongside
	// an inline scan.
	llmAnalyzerStreamExecutionPath = "llm_analyzer_stream"
	// llmShadowStreamExecutionPath marks analysis requests the fine-tuned LLM
	// analyzer evaluates in the shadow engine mode: the legacy engines scan
	// the same batch inline and enforce, and the model's verdict is recorded
	// for comparison only.
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

// batchScanRequestID preserves the transport correlation identity used by
// already-published requests. The request id is deterministic over the exact
// Temporal batch plus its standard/prompt-policy lane, so activity retries and
// rolling deployments converge on the same downstream finding ids.
func batchScanRequestID(args AnalyzeBatchArgs, discriminator string) uuid.UUID {
	parts := []string{
		discriminator,
		args.ProjectID.String(),
		args.RiskPolicyID.String(),
		strconv.FormatInt(args.PolicyVersion, 10),
	}
	for _, id := range args.MessageIDs {
		parts = append(parts, id.String())
	}
	for _, id := range args.ContentPartIDs {
		parts = append(parts, id.String())
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:scanrequest:"+strings.Join(parts, "\x00")))
}

func nilUUIDString(id uuid.UUID) *string {
	if id == uuid.Nil {
		return nil
	}
	value := id.String()
	return &value
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

func (a *AnalyzeBatch) recordBatchResults(
	ctx context.Context,
	definition metering.Definition,
	args AnalyzeBatchArgs,
	messages []batchMessage,
	results []scanners.Result,
	startedAt time.Time,
) {
	requestID := batchScanRequestID(args, "standard").String()
	for i := range min(len(messages), len(results)) {
		if !results[i].Completed {
			continue
		}
		if err := a.riskRecorder.Record(
			ctx,
			definition,
			batchRiskProvenance(args, messages[i], inlineBatchExecutionPath, requestID),
			results[i].STokens,
			startedAt,
		); err != nil {
			a.logger.ErrorContext(ctx, "record batch risk scan usage", attr.SlogError(err))
		}
	}
}

func findingsFromResults(results []scanners.Result) [][]scanners.Finding {
	findings := make([][]scanners.Finding, len(results))
	for i := range results {
		findings[i] = results[i].Findings
	}
	return findings
}

func (a *AnalyzeBatch) scanStandardPolicy(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, customRuleIDs []string, exclusions ExclusionSet, masks CategoryScopeMasks) ([][]scanners.Finding, error) {
	ctx, scanSpan := a.tracer.Start(ctx, "risk.scanMessages")
	defer scanSpan.End()
	activity.RecordHeartbeat(ctx, 0)

	contents := messageContents(messages)

	sources := newSourceSet(args.Sources)
	n := len(messages)
	gitleaksFindings := make([][]scanners.Finding, n)
	presidioFindings := make([][]scanners.Finding, n)
	shadowMCPFindings := make([][]scanners.Finding, n)
	destructiveToolFindings := make([][]scanners.Finding, n)
	cliDestructiveFindings := make([][]scanners.Finding, n)
	customFindings := make([][]scanners.Finding, n)

	// The risk engine mode selects the engine behind every covered source
	// (gitleaks, presidio, prompt_injection, destructive_tool,
	// cli_destructive). In the llm mode the fine-tuned model's async lane
	// replaces the legacy engines: no inline scan, no legacy analysis
	// request, and no Postgres rows for those sources, whose findings are
	// ClickHouse-only. In the shadow mode the legacy engines run exactly as
	// in the off mode and the same messages are also published to the
	// model's lane, marked shadow, so both engines' verdicts can be compared
	// on identical traffic without the model ever enforcing. Custom rules,
	// shadow_mcp and account_identity keep their engines in every mode.
	//
	// Both model modes only publish when this worker knows an analyzer is
	// configured: with GRAM_RISK_LLM_URL empty the streams consumer acks
	// every request without findings. The llm mode then falls back to the
	// legacy engines rather than leave the organization without async
	// coverage; the shadow mode just skips the comparison lane.
	llmMode := false
	llmShadow := false
	llmOrgSlug := ""
	if llmanalyzer.CoversAnySource(args.Sources) {
		mode, orgSlug := policyflags.ProjectFlagMode(ctx, a.logger, repo.New(a.db), a.flags, args.OrganizationID, args.ProjectID, feature.FlagRiskLLMAnalyzer)
		switch {
		case mode == feature.VariantRiskLLMLLM && a.llmAnalyzerEnabled:
			llmMode, llmOrgSlug = true, orgSlug
		case mode == feature.VariantRiskLLMLLM:
			a.llmFallbackOnce.Do(func() {
				a.logger.WarnContext(ctx, "LLM analyzer mode llm but GRAM_RISK_LLM_URL empty; batch scans fall back to legacy engines",
					attr.SlogOrganizationID(args.OrganizationID),
					attr.SlogRiskPolicyID(args.RiskPolicyID.String()),
				)
			})
			a.metrics.RecordLLMPolicyEvaluation(ctx, args.OrganizationID, args.RiskPolicyID.String(), llmPolicyEvaluationFallbackLegacy, 1)
		case mode == feature.VariantRiskLLMShadow && a.llmAnalyzerEnabled:
			llmShadow, llmOrgSlug = true, orgSlug
		case mode == feature.VariantRiskLLMShadow:
			a.metrics.RecordLLMPolicyEvaluation(ctx, args.OrganizationID, args.RiskPolicyID.String(), llmPolicyEvaluationShadowSkipped, 1)
		}
	}

	var wg sync.WaitGroup
	var gitleaksErr error
	var presidioPublishErr error
	var presidioErr error
	var llmPublishErr error
	var llmPublishFailed int

	var promptInjectionPublishErr error
	var customErr error

	if llmMode || llmShadow {
		wg.Go(func() {
			llmPublishFailed, llmPublishErr = a.publishLLMScanRequests(ctx, args, messages, llmOrgSlug, llmCoveredSources(sources), masks, llmShadow)
		})
	}

	if !llmMode && sources.Has(SourceGitleaks) {
		wg.Go(func() {
			findings, err := a.scanGitleaks(ctx, args, messages, contents)
			if err != nil {
				gitleaksErr = err
				return
			}
			gitleaksFindings = findings
		})
	}

	if !llmMode && sources.Has(SourcePresidio) {
		wg.Go(func() {
			subMessages, subContents, indices := masks.Subset(messages, contents, sourceCategories[SourcePresidio])
			a.metrics.RecordRecommendedScopePrefiltered(ctx, args.OrganizationID, SourcePresidio, masks.RecommendedPrefilteredCount(sourceCategories[SourcePresidio]))
			// The publish must succeed for the activity to succeed (the async
			// analyzer feeds the ClickHouse findings store), while the inline
			// scan below tolerates partial results.
			scoreThreshold := resolvePresidioScoreThreshold(args.PresidioScoreThreshold)
			if err := a.publishPresidioScanRequests(ctx, args, subMessages, scoreThreshold); err != nil {
				presidioPublishErr = err
				return
			}
			startedAt := time.Now().UTC()
			results, err := a.scanPresidio(ctx, args, scoreThreshold, subMessages, subContents)
			a.recordBatchResults(ctx, metering.RiskPresidio(), args, subMessages, results, startedAt)
			presidioFindings = scatterFindings(n, indices, findingsFromResults(results))
			if err != nil {
				presidioErr = err
			}
		})
	}

	// Prompt injection is dispatched, not scanned: the streams consumer is the
	// only engine, so this lane contributes no findings to the merge below and
	// no prompt_injection rows to Postgres. Its findings reach ClickHouse
	// risk_findings from the consumer, so an org needs
	// risk-list-from-clickhouse / risk-overview-from-clickhouse to see them —
	// the same prerequisite the LLM analyzer's async lane documents.
	if !llmMode && sources.Has(SourcePromptInjection) {
		wg.Go(func() {
			subMessages, _, _ := masks.Subset(messages, contents, sourceCategories[SourcePromptInjection])
			a.metrics.RecordRecommendedScopePrefiltered(ctx, args.OrganizationID, SourcePromptInjection, masks.RecommendedPrefilteredCount(sourceCategories[SourcePromptInjection]))
			promptInjectionPublishErr = a.dispatchPromptInjection(ctx, args, subMessages)
		})
	}

	if len(customRuleIDs) > 0 {
		wg.Go(func() {
			findings, err := a.scanCustomRules(ctx, args, messages, customRuleIDs)
			if err != nil {
				customErr = err
				return
			}
			customFindings = findings
		})
	}

	wg.Wait()

	if gitleaksErr != nil {
		scanSpan.SetStatus(codes.Error, gitleaksErr.Error())
		return nil, fmt.Errorf("gitleaks scan batch: %w", gitleaksErr)
	}
	if presidioPublishErr != nil {
		scanSpan.SetStatus(codes.Error, presidioPublishErr.Error())
		return nil, fmt.Errorf("presidio scan dispatch: %w", presidioPublishErr)
	}
	if llmPublishErr != nil && llmShadow {
		// The shadow lane only compares: a dropped shadow request costs the
		// comparison one message, while failing the activity would retry the
		// legacy engines' inline scan and hold their findings back behind an
		// LLM transport outage. Count the messages that did not reach the
		// topic (the acknowledged ones were counted as shadow_published) and
		// keep the legacy results.
		a.logger.WarnContext(ctx, "shadow LLM analyzer scan dispatch failed for some messages; keeping legacy engine results",
			attr.SlogError(llmPublishErr),
			attr.SlogOrganizationID(args.OrganizationID),
			attr.SlogRiskPolicyID(args.RiskPolicyID.String()),
			attr.SlogRiskLLMPublishFailedCount(llmPublishFailed),
		)
		a.metrics.RecordLLMPolicyEvaluation(ctx, args.OrganizationID, args.RiskPolicyID.String(), llmPolicyEvaluationShadowPublishError, llmPublishFailed)
		llmPublishErr = nil
	}
	if llmPublishErr != nil {
		scanSpan.SetStatus(codes.Error, llmPublishErr.Error())
		return nil, fmt.Errorf("llm analyzer scan dispatch: %w", llmPublishErr)
	}
	if promptInjectionPublishErr != nil {
		scanSpan.SetStatus(codes.Error, promptInjectionPublishErr.Error())
		return nil, fmt.Errorf("prompt injection scan dispatch: %w", promptInjectionPublishErr)
	}
	if customErr != nil {
		scanSpan.SetStatus(codes.Error, customErr.Error())
		return nil, fmt.Errorf("custom rule scan: %w", customErr)
	}
	if ctx.Err() != nil {
		err := fmt.Errorf("scan canceled: %w", ctx.Err())
		if presidioErr != nil {
			err = errors.Join(err, fmt.Errorf("presidio: %w", presidioErr))
		}
		scanSpan.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	if sources.Has(shadowmcp.SourceShadowMCP) {
		shadowMCPFindings = a.scanShadowMCP(ctx, args.OrganizationID, args.ProjectID, args.RiskPolicyID, messages)
		activity.RecordHeartbeat(ctx, shadowmcp.SourceShadowMCP)
	}
	if !llmMode && sources.Has(shadowmcp.SourceDestructiveTool) {
		destructiveToolFindings = a.scanDestructiveToolAnnotations(ctx, args.OrganizationID, messages)
		activity.RecordHeartbeat(ctx, shadowmcp.SourceDestructiveTool)
	}
	if !llmMode && sources.Has(SourceCLIDestructive) {
		findings, err := a.scanDestructiveCLICommands(ctx, args, messages)
		if err != nil {
			scanSpan.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		cliDestructiveFindings = findings
		activity.RecordHeartbeat(ctx, SourceCLIDestructive)
	}

	// Note: SourceAccountIdentity is deliberately absent here — it is
	// session-scoped and evaluated in Do over the batch's full message-id set,
	// bypassing the message-type filter and CEL scope that shape `messages`.

	return mergeFindings(mergeFindingsInput{
		orgID:                   args.OrganizationID,
		metrics:                 a.metrics,
		masks:                   masks,
		exclusions:              exclusions,
		builtinEnabled:          args.BuiltinPresetsEnabled,
		builtinPresets:          a.builtinPresets,
		gitleaksFindings:        gitleaksFindings,
		presidioFindings:        presidioFindings,
		shadowMCPFindings:       shadowMCPFindings,
		destructiveToolFindings: destructiveToolFindings,
		cliDestructiveFindings:  cliDestructiveFindings,
		customFindings:          customFindings,
	}, ctx), nil
}

type mergeFindingsInput struct {
	orgID                   string
	metrics                 *riskMetrics
	masks                   CategoryScopeMasks
	exclusions              ExclusionSet
	builtinEnabled          bool
	builtinPresets          *presetlib.Library
	gitleaksFindings        [][]scanners.Finding
	presidioFindings        [][]scanners.Finding
	shadowMCPFindings       [][]scanners.Finding
	destructiveToolFindings [][]scanners.Finding
	cliDestructiveFindings  [][]scanners.Finding
	customFindings          [][]scanners.Finding
}

func mergeFindings(in mergeFindingsInput, ctx context.Context) [][]scanners.Finding {
	merged := make([][]scanners.Finding, len(in.gitleaksFindings))
	for i := range merged {
		combined := concatFindings(
			in.gitleaksFindings[i],
			in.presidioFindings[i],
			in.shadowMCPFindings[i],
			in.destructiveToolFindings[i],
			in.cliDestructiveFindings[i],
			in.customFindings[i],
		)
		combined = filterByCategoryScopes(ctx, in.orgID, in.metrics, in.masks, i, combined)
		if !in.exclusions.Empty() {
			combined = in.exclusions.FilterFindings(combined)
		}
		if in.builtinEnabled {
			combined = dropBuiltinFalsePositives(in.builtinPresets, combined)
		}
		merged[i] = dedup(combined)
	}
	return merged
}

func filterByCategoryScopes(ctx context.Context, orgID string, metrics *riskMetrics, masks CategoryScopeMasks, i int, in []scanners.Finding) []scanners.Finding {
	if len(in) == 0 {
		return in
	}
	out := make([]scanners.Finding, 0, len(in))
	for _, finding := range in {
		cat := categoryForFinding(finding)
		if masks.InScope(i, cat) {
			out = append(out, finding)
			continue
		}
		metrics.RecordRecommendedScopeSuppressed(ctx, orgID, cat)
	}
	return out
}

func messageContents(messages []batchMessage) []string {
	contents := make([]string, len(messages))
	for i, msg := range messages {
		contents[i] = msg.scanSurface()
	}
	return contents
}

func concatFindings(groups ...[]scanners.Finding) []scanners.Finding {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	out := make([]scanners.Finding, 0, total)
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}
