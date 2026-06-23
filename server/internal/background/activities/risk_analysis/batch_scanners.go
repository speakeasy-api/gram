package risk_analysis

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"go.opentelemetry.io/otel/codes"
	"go.temporal.io/sdk/activity"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func (a *AnalyzeBatch) scan(ctx context.Context, args AnalyzeBatchArgs, messages []repo.GetMessageContentBatchRow, customRules []CompiledCELRule, exclusions ExclusionSet, scopeExcluded []bool) ([][]Finding, error) {
	ctx, scanSpan := a.tracer.Start(ctx, "risk.scanMessages")
	defer scanSpan.End()
	activity.RecordHeartbeat(ctx, 0)

	n := len(messages)
	contents := make([]string, n)
	msgids := make([]uuid.UUID, n)
	for i, msg := range messages {
		contents[i] = msg.Content
		msgids[i] = msg.ID
	}

	requestID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate scan request id: %w", err)
	}

	gitleaksFindings := make([][]Finding, n)
	presidioFindings := make([][]Finding, n)
	shadowMCPFindings := make([][]Finding, n)
	destructiveToolFindings := make([][]Finding, n)
	cliDestructiveFindings := make([][]Finding, n)
	promptInjectionFindings := make([][]Finding, n)
	customFindings := make([][]Finding, n)

	var wg sync.WaitGroup
	var gitleaksErr error
	var presidioErr error
	var customErr error

	if slices.Contains(args.Sources, "gitleaks") {
		wg.Go(func() {
			createdAt := time.Now().UTC().Format(time.RFC3339)
			publishResults := make([]gcp.PublishResult, len(contents))
			for i, content := range contents {
				publishResults[i] = a.gitleaksPub.Publish(ctx, riskv1.GitleaksAnalysis_builder{
					RequestId:         new(requestID.String()),
					ChatMessageId:     new(msgids[i].String()),
					ProjectId:         new(args.ProjectID.String()),
					OrganizationId:    &args.OrganizationID,
					RiskPolicyId:      new(args.RiskPolicyID.String()),
					RiskPolicyVersion: &args.PolicyVersion,
					CreatedAt:         &createdAt,

					ReplyUrn: nil,
					Content:  new(content),
				}.Build())
			}
			drainPublishAcks(ctx, a.logger, "failed to publish gitleaks scan request", publishResults)

			results, err := a.scanner.ScanBatchParallel(contents)
			if err != nil {
				gitleaksErr = err
				return
			}
			for i, detections := range results {
				gitleaksFindings[i] = fromDetections(detections)
			}
		})
	}

	if slices.Contains(args.Sources, "presidio") {
		wg.Go(func() {
			createdAt := time.Now().UTC().Format(time.RFC3339)
			publishResults := make([]gcp.PublishResult, len(contents))
			for i, content := range contents {
				publishResults[i] = a.presidioPub.Publish(ctx, riskv1.PresidioAnalysis_builder{
					RequestId:         new(requestID.String()),
					ChatMessageId:     new(msgids[i].String()),
					ProjectId:         new(args.ProjectID.String()),
					OrganizationId:    &args.OrganizationID,
					RiskPolicyId:      new(args.RiskPolicyID.String()),
					RiskPolicyVersion: &args.PolicyVersion,
					CreatedAt:         &createdAt,

					ReplyUrn: nil,
					Content:  &content,
					Entities: args.PresidioEntities,
				}.Build())
			}
			drainPublishAcks(ctx, a.logger, "failed to publish presidio scan request", publishResults)

			results, err := a.piiScanner.AnalyzeBatch(ctx, contents, args.PresidioEntities, func() {
				activity.RecordHeartbeat(ctx, "presidio")
			})
			if results != nil {
				presidioFindings = results
			}
			if err != nil {
				presidioErr = err
				a.logger.WarnContext(ctx, "presidio scan returned errors, using partial results", attr.SlogError(err))
				if a.metrics.presidioScanSkipped != nil {
					a.metrics.presidioScanSkipped.Add(ctx, 1)
				}
			}
		})
	}

	if slices.Contains(args.Sources, SourcePromptInjection) {
		l1Enabled := a.projectFlagEnabled(ctx, args.OrganizationID, args.ProjectID, feature.FlagPromptInjectionUseClassifier)
		var msgs []JudgeMessage
		if l1Enabled {
			msgs = make([]JudgeMessage, len(messages))
			for i := range messages {
				msgs[i] = a.judgeMessageForRow(ctx, messages[i])
			}
		}
		wg.Go(func() {
			results, err := a.piScanner.ScanBatch(ctx, contents, args.OrganizationID, args.ProjectID.String(), msgs, l1Enabled)
			if err != nil {
				a.logger.WarnContext(ctx, "prompt injection scan failed", attr.SlogError(err))
				return
			}
			promptInjectionFindings = results
			activity.RecordHeartbeat(ctx, "prompt_injection")
		})
	}

	if len(customRules) > 0 {
		wg.Go(func() {
			eng := a.celEng
			for i, msg := range messages {
				findings, err := ScanCELRules(eng, a.customRuleMessageView(ctx, msg), customRules)
				if err != nil {
					customErr = err
					return
				}
				customFindings[i] = findings
			}
			activity.RecordHeartbeat(ctx, "custom")
		})
	}

	wg.Wait()

	if gitleaksErr != nil {
		scanSpan.SetStatus(codes.Error, gitleaksErr.Error())
		return nil, fmt.Errorf("gitleaks scan batch: %w", gitleaksErr)
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

	if slices.Contains(args.Sources, shadowmcp.SourceShadowMCP) {
		shadowMCPFindings = a.scanShadowMCP(ctx, args.OrganizationID, args.ProjectID, messages)
		activity.RecordHeartbeat(ctx, "shadow_mcp")
	}

	if slices.Contains(args.Sources, shadowmcp.SourceDestructiveTool) {
		destructiveToolFindings = a.scanDestructiveToolAnnotations(ctx, args.OrganizationID, messages)
		activity.RecordHeartbeat(ctx, "destructive_tool")
	}

	if slices.Contains(args.Sources, SourceCLIDestructive) {
		cliDestructiveFindings = a.scanDestructiveCLICommands(ctx, messages)
		activity.RecordHeartbeat(ctx, "cli_destructive")
	}

	merged := make([][]Finding, n)
	for i := range n {
		if scopeExcluded != nil && scopeExcluded[i] {
			merged[i] = nil
			continue
		}
		combined := slices.Concat(gitleaksFindings[i], presidioFindings[i], shadowMCPFindings[i], destructiveToolFindings[i], cliDestructiveFindings[i], promptInjectionFindings[i], customFindings[i])
		if !exclusions.Empty() {
			combined = exclusions.FilterFindings(combined)
		}
		merged[i] = dedup(combined)
	}
	return merged, nil
}

func (a *AnalyzeBatch) scopeExclusions(ctx context.Context, scope CompiledScope, messages []repo.GetMessageContentBatchRow) []bool {
	if !scope.Active() {
		return nil
	}
	excluded := make([]bool, len(messages))
	for i, msg := range messages {
		view := a.customRuleMessageView(ctx, msg)
		excluded[i] = !scope.Includes(view) || scope.Exempts(view)
	}
	return excluded
}
