package risk_analysis

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func (a *AnalyzeBatch) scanStandardPolicy(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, customRuleIDs []string, exclusions ExclusionSet, masks CategoryScopeMasks) ([][]scanners.Finding, error) {
	ctx, scanSpan := a.tracer.Start(ctx, "risk.scanMessages")
	defer scanSpan.End()
	activity.RecordHeartbeat(ctx, 0)

	contents := messageContents(messages)
	requestID, err := uuid.NewV7()
	if err != nil {
		scanSpan.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("generate scan request id: %w", err)
	}

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
	var presidioErr error
	var customErr error

	if sources.Has(SourceGitleaks) {
		wg.Go(func() {
			findings, err := a.scanGitleaks(ctx, args, requestID, messages, contents)
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
			findings, err := a.scanPresidio(ctx, args, requestID, subMessages, subContents)
			presidioFindings = scatterFindings(n, indices, findings)
			if err != nil {
				presidioErr = err
			}
		})
	}

	if sources.Has(SourcePromptInjection) {
		wg.Go(func() {
			subMessages, subContents, indices := masks.Subset(messages, contents, sourceCategories[SourcePromptInjection])
			a.metrics.RecordRecommendedScopePrefiltered(ctx, args.OrganizationID, SourcePromptInjection, masks.RecommendedPrefilteredCount(sourceCategories[SourcePromptInjection]))
			findings := a.scanPromptInjection(ctx, args, requestID, subMessages, subContents)
			promptInjectionFindings = scatterFindings(n, indices, findings)
		})
	}

	if len(customRuleIDs) > 0 {
		wg.Go(func() {
			findings, err := a.scanCustomRules(ctx, args, requestID, messages, customRuleIDs)
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
		shadowMCPFindings = a.scanShadowMCP(ctx, args.OrganizationID, args.ProjectID, messages)
		activity.RecordHeartbeat(ctx, shadowmcp.SourceShadowMCP)
	}
	if sources.Has(shadowmcp.SourceDestructiveTool) {
		destructiveToolFindings = a.scanDestructiveToolAnnotations(ctx, args.OrganizationID, messages)
		activity.RecordHeartbeat(ctx, shadowmcp.SourceDestructiveTool)
	}
	if sources.Has(SourceCLIDestructive) {
		cliDestructiveFindings = a.scanDestructiveCLICommands(ctx, messages)
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
