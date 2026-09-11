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
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

const (
	inlineBatchExecutionPath  = "inline_batch"
	shadowStreamExecutionPath = "shadow_stream"
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
	promptInjectionFindings := make([][]scanners.Finding, n)
	customFindings := make([][]scanners.Finding, n)

	var wg sync.WaitGroup
	var gitleaksErr error
	var presidioPublishErr error
	var presidioErr error

	var promptInjectionErr error
	var customErr error

	if sources.Has(SourceGitleaks) {
		wg.Go(func() {
			findings, err := a.scanGitleaks(ctx, args, messages, contents)
			if err != nil {
				gitleaksErr = err
				return
			}
			gitleaksFindings = findings
		})
	}

	if sources.Has(SourcePresidio) {
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

	if sources.Has(SourcePromptInjection) {
		wg.Go(func() {
			subMessages, subContents, indices := masks.Subset(messages, contents, sourceCategories[SourcePromptInjection])
			a.metrics.RecordRecommendedScopePrefiltered(ctx, args.OrganizationID, SourcePromptInjection, masks.RecommendedPrefilteredCount(sourceCategories[SourcePromptInjection]))
			findings, err := a.scanPromptInjection(ctx, args, subMessages, subContents)
			if err != nil {
				promptInjectionErr = err
				return
			}
			promptInjectionFindings = scatterFindings(n, indices, findings)
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
	if promptInjectionErr != nil {
		scanSpan.SetStatus(codes.Error, promptInjectionErr.Error())
		return nil, fmt.Errorf("prompt injection scan dispatch: %w", promptInjectionErr)
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
	if sources.Has(shadowmcp.SourceDestructiveTool) {
		destructiveToolFindings = a.scanDestructiveToolAnnotations(ctx, args.OrganizationID, messages)
		activity.RecordHeartbeat(ctx, shadowmcp.SourceDestructiveTool)
	}
	if sources.Has(SourceCLIDestructive) {
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
		promptInjectionFindings: promptInjectionFindings,
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
	promptInjectionFindings [][]scanners.Finding
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
			in.promptInjectionFindings[i],
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
		if len(masks.policyOut) == 0 || !masks.policyOut[i] {
			metrics.RecordRecommendedScopeSuppressed(ctx, orgID, cat)
		}
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
