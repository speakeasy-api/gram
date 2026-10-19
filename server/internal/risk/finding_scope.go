package risk

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/message"
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
// scopes are cached for a minute, keyed by policy version.
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

// scopeMessageView rebuilds the message view the scopes evaluate: content,
// type and one tool view per call; a single tool request carried as
// tool_name and body becomes one tool view, as in realtimeMessageView.
func scopeMessageView(target llmanalyzer.ScopeTarget) risk_analysis.MessageView {
	view := risk_analysis.MessageView{Content: target.Content, Type: target.MessageType, Tools: []risk_analysis.ToolView{}}
	for _, call := range target.ToolCalls {
		view.Tools = append(view.Tools, risk_analysis.NewToolView(call.Name, call.Arguments))
	}
	if len(view.Tools) == 0 && view.Type == message.ToolRequest && target.ToolName != "" {
		view.Tools = []risk_analysis.ToolView{risk_analysis.NewToolView(target.ToolName, target.Body)}
	}
	return view
}
