package risk_analysis

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// publishBatch hands the batch to the Pub/Sub analysis handlers, which produce
// every finding and usage reading for an organization on the ClickHouse
// findings path. It scans nothing inline and writes nothing to Postgres. It
// fails unless every scan request is acknowledged, and the coordinator marks
// the batch analyzed only after it succeeds, so a dropped request is redriven
// rather than lost. Disabled rules, builtin presets and exclusions are applied
// by the ClickHouse findings writer. It returns the number of units fetched.
func (a *AnalyzeBatch) publishBatch(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy) (int, error) {
	activity.RecordHeartbeat(ctx, 0)

	if len(args.MessageIDs) > 0 && newSourceSet(args.Sources).Has(SourceAccountIdentity) {
		if _, err := a.publishAccountIdentityScanRequests(ctx, args); err != nil {
			return 0, err
		}
	}

	messages, err := a.fetchContent(ctx, args)
	if err != nil {
		return 0, err
	}
	if len(messages) == 0 {
		return 0, nil
	}

	masks, err := a.categoryScopeMasks(ctx, args, messages)
	if err != nil {
		return 0, err
	}

	switch policy.PolicyType {
	case PolicyTypePromptBased:
		err = a.publishPromptPolicy(ctx, args, policy, messages, masks)
	default:
		err = a.publishStandardPolicy(ctx, args, messages, policy.CustomRuleIds, masks)
	}
	if err != nil {
		return 0, err
	}
	return len(messages), nil
}

// publishPromptPolicy publishes the in-scope messages for the handler to judge.
// A policy without a prompt publishes nothing.
func (a *AnalyzeBatch) publishPromptPolicy(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []batchMessage, masks CategoryScopeMasks) error {
	if !hasPrompt(policy) || !a.projectFlagEnabled(ctx, args.OrganizationID, args.ProjectID, feature.FlagPromptPolicies) {
		return nil
	}
	indices := promptPolicyIndices(messages, masks)
	if len(indices) == 0 {
		return nil
	}
	return a.publishPromptPolicyScanRequests(ctx, args, policy, messages, indices, true)
}

// publishStandardPolicy publishes each source's in-scope messages to its
// analysis handler. The LLM analyzer lane, when enforcing, stands in for the
// sources the model enforces exactly as it does for the inline scan.
func (a *AnalyzeBatch) publishStandardPolicy(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, customRuleIDs []string, masks CategoryScopeMasks) error {
	sources := newSourceSet(args.Sources)
	lane := a.resolveLLMLane(ctx, args)
	runs := func(source string) bool { return lane.runs(sources, source) }
	inScope := func(source string) []batchMessage {
		cats := sourceCategories[source]
		a.metrics.RecordRecommendedScopePrefiltered(ctx, args.OrganizationID, source, masks.RecommendedPrefilteredCount(cats))
		subMessages := make([]batchMessage, 0, len(messages))
		for i, msg := range messages {
			if masks.AdmitsAny(i, cats) {
				subMessages = append(subMessages, msg)
			}
		}
		return subMessages
	}

	var group errgroup.Group
	group.Go(func() error {
		return a.publishLLMLane(ctx, args, messages, lane, sources, masks)
	})
	if sources.Has(shadowmcp.SourceShadowMCP) || runs(shadowmcp.SourceDestructiveTool) || runs(SourceCLIDestructive) {
		group.Go(func() error {
			return a.publishToolCallScanRequests(ctx, args, messages, sources, masks, lane.ownEngineRuns(shadowmcp.SourceDestructiveTool), lane.ownEngineRuns(SourceCLIDestructive))
		})
	}
	if runs(SourceGitleaks) {
		subMessages := inScope(SourceGitleaks)
		group.Go(func() error {
			return a.publishGitleaksScanRequests(ctx, args, subMessages)
		})
	}
	if runs(SourcePresidio) {
		subMessages := inScope(SourcePresidio)
		group.Go(func() error {
			return a.publishPresidioScanRequests(ctx, args, subMessages, resolvePresidioScoreThreshold(args.PresidioScoreThreshold))
		})
	}
	if runs(SourcePromptInjection) {
		subMessages := inScope(SourcePromptInjection)
		group.Go(func() error {
			return a.publishPromptInjectionScanRequests(ctx, args, subMessages, true)
		})
	}
	if len(customRuleIDs) > 0 {
		subMessages := inScope(SourceCustom)
		group.Go(func() error {
			return a.publishCustomRulesScanRequests(ctx, args, subMessages, customRuleIDs)
		})
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("publish scan requests: %w", err)
	}
	return nil
}

// categoryScopeMasks compiles the policy's detection scopes against the batch.
func (a *AnalyzeBatch) categoryScopeMasks(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage) (CategoryScopeMasks, error) {
	specified, err := CompileDetectionScopes(a.celEng, args.DetectionScopes)
	if err != nil {
		return CategoryScopeMasks{}, fmt.Errorf("compile detection scopes: %w", err)
	}
	return NewCategoryScopes(a.recommended, specified, a.metrics).Masks(ctx, messages), nil
}
