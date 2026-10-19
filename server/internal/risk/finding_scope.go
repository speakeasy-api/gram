package risk

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

const (
	findingScopeCacheSize = 1000
	findingScopeCacheTTL  = time.Minute
)

type findingScopeKey struct {
	projectID uuid.UUID
	policyID  uuid.UUID
	version   int64
}

// FindingScopeResolver applies a policy's detection scopes to the LLM
// analyzer's findings after the scan. The legacy engines are prefiltered by
// the same scopes; the model sees every message and is pruned here.
// Implements llmanalyzer.FindingScopeFilter.
type FindingScopeResolver struct {
	db          repo.DBTX
	celEng      *celenv.Engine
	recommended risk_analysis.RecommendedSet
	cache       *expirable.LRU[findingScopeKey, risk_analysis.CategoryScope]
}

// NewFindingScopeResolver compiles the recommended scopes once; per-policy
// scopes are cached for a minute. The request's policy version is part of
// the cache key only so an edited policy is reloaded at once: the policy's
// current row is what is loaded, as the batch activity itself does.
func NewFindingScopeResolver(db repo.DBTX, celEng *celenv.Engine) (*FindingScopeResolver, error) {
	recommended, err := risk_analysis.CompileRecommended(celEng)
	if err != nil {
		return nil, fmt.Errorf("compile recommended scopes version %d: %w", recommendedscopes.Version, err)
	}
	return &FindingScopeResolver{
		db:          db,
		celEng:      celEng,
		recommended: recommended,
		cache:       expirable.NewLRU[findingScopeKey, risk_analysis.CategoryScope](findingScopeCacheSize, nil, findingScopeCacheTTL),
	}, nil
}

// FilterFindings drops findings whose category the scopes exclude for the
// message. Lookup errors are returned for the caller to handle.
func (r *FindingScopeResolver) FilterFindings(ctx context.Context, target llmanalyzer.ScopeTarget, findings []scanners.Finding) ([]scanners.Finding, error) {
	if len(findings) == 0 {
		return findings, nil
	}
	projectID, err := uuid.Parse(target.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse analysis project id: %w", err)
	}
	policyID, err := uuid.Parse(target.RiskPolicyID)
	if err != nil {
		return nil, fmt.Errorf("parse analysis risk policy id: %w", err)
	}
	scope, err := r.scopeFor(ctx, projectID, policyID, target.RiskPolicyVersion)
	if err != nil {
		return nil, err
	}
	return scope.FilterFindings(scopeMessageView(target), findings), nil
}

func (r *FindingScopeResolver) scopeFor(ctx context.Context, projectID, policyID uuid.UUID, version int64) (risk_analysis.CategoryScope, error) {
	key := findingScopeKey{projectID: projectID, policyID: policyID, version: version}
	if scope, ok := r.cache.Get(key); ok {
		return scope, nil
	}
	if r.db == nil {
		return risk_analysis.CategoryScope{}, errors.New("finding scope database is unavailable")
	}
	policy, err := repo.New(r.db).GetRiskPolicy(ctx, repo.GetRiskPolicyParams{ID: policyID, ProjectID: projectID})
	if err != nil {
		return risk_analysis.CategoryScope{}, fmt.Errorf("load risk policy detection scopes: %w", err)
	}
	specified, err := risk_analysis.CompileDetectionScopes(r.celEng, risk_analysis.DetectionScopesFromConfig(policy.AnalyzerConfig))
	if err != nil {
		return risk_analysis.CategoryScope{}, fmt.Errorf("compile detection scopes: %w", err)
	}
	scope := risk_analysis.NewCategoryScope(r.recommended, specified)
	r.cache.Add(key, scope)
	return scope, nil
}

// scopeMessageView rebuilds the message view the batch prefilter gave the
// legacy engines for the same message (batchMessageView): the message's own
// text as content and, for a tool request, one tool view per structured
// call. Tool arguments are reachable only through tool_calls, never through
// content, so a scope over content cannot match them.
func scopeMessageView(target llmanalyzer.ScopeTarget) risk_analysis.MessageView {
	calls := make([]risk_analysis.ToolCallInput, 0, len(target.ToolCalls))
	for _, call := range target.ToolCalls {
		calls = append(calls, risk_analysis.ToolCallInput{Name: call.Name, Arguments: call.Arguments})
	}
	return risk_analysis.NewMessageView(target.Content, target.MessageType, calls)
}
