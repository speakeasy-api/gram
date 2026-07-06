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
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func (a *AnalyzeBatch) scanStandardPolicy(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, customRules []CompiledCELRule, exclusions ExclusionSet, outOfPolicyScope []bool) ([][]Finding, error) {
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
			findings, err := a.scanPresidio(ctx, args, requestID, messages, contents)
			presidioFindings = findings
			if err != nil {
				presidioErr = err
			}
		})
	}

	if sources.Has(SourcePromptInjection) {
		wg.Go(func() {
			promptInjectionFindings = a.scanPromptInjection(ctx, args, messages, contents)
		})
	}

	if len(customRules) > 0 {
		wg.Go(func() {
			findings, err := a.scanCustomRules(ctx, args, requestID, messages, customRules)
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

	return mergeFindings(mergeFindingsInput{
		outOfPolicyScope:        outOfPolicyScope,
		exclusions:              exclusions,
		builtinEnabled:          args.BuiltinPresetsEnabled,
		builtinPresets:          a.builtinExclusionsPresets,
		gitleaksFindings:        gitleaksFindings,
		presidioFindings:        presidioFindings,
		shadowMCPFindings:       shadowMCPFindings,
		destructiveToolFindings: destructiveToolFindings,
		cliDestructiveFindings:  cliDestructiveFindings,
		promptInjectionFindings: promptInjectionFindings,
		customFindings:          customFindings,
	}), nil
}

type mergeFindingsInput struct {
	outOfPolicyScope        []bool
	exclusions              ExclusionSet
	builtinEnabled          bool
	builtinPresets          *presetlib.Library
	gitleaksFindings        [][]Finding
	presidioFindings        [][]Finding
	shadowMCPFindings       [][]Finding
	destructiveToolFindings [][]Finding
	cliDestructiveFindings  [][]Finding
	promptInjectionFindings [][]Finding
	customFindings          [][]Finding
}

func mergeFindings(in mergeFindingsInput) [][]Finding {
	merged := make([][]Finding, len(in.gitleaksFindings))
	for i := range merged {
		if len(in.outOfPolicyScope) > 0 && in.outOfPolicyScope[i] {
			continue
		}
		combined := concatFindings(
			in.gitleaksFindings[i],
			in.presidioFindings[i],
			in.shadowMCPFindings[i],
			in.destructiveToolFindings[i],
			in.cliDestructiveFindings[i],
			in.promptInjectionFindings[i],
			in.customFindings[i],
		)
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

func messageContents(messages []batchMessage) []string {
	contents := make([]string, len(messages))
	for i, msg := range messages {
		contents[i] = msg.Content
	}
	return contents
}

func concatFindings(groups ...[]Finding) []Finding {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	out := make([]Finding, 0, total)
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

func (a *AnalyzeBatch) scopeExclusions(_ context.Context, scope CompiledScope, messages []batchMessage) []bool {
	if !scope.Active() {
		return []bool{}
	}
	excluded := make([]bool, len(messages))
	for i, msg := range messages {
		view := batchMessageView(msg)
		excluded[i] = !scope.Includes(view) || scope.Exempts(view)
	}
	return excluded
}
