package risk

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const (
	findingPolicyFilterCacheSize = 1000
	findingPolicyFilterCacheTTL  = time.Minute
)

type findingPolicyFilterKey struct {
	projectID uuid.UUID
	policyID  uuid.UUID
}

type findingPolicyFilters struct {
	disabledRules         risk_analysis.DisabledRuleSet
	builtinPresetsEnabled bool
}

func emptyFindingPolicyFilters() findingPolicyFilters {
	return findingPolicyFilters{
		disabledRules:         risk_analysis.NewDisabledRuleSet(nil),
		builtinPresetsEnabled: false,
	}
}

type findingPolicyFilterResolver struct {
	db    repo.DBTX
	cache *expirable.LRU[findingPolicyFilterKey, findingPolicyFilters]
}

func newFindingPolicyFilterResolver(db repo.DBTX) *findingPolicyFilterResolver {
	return &findingPolicyFilterResolver{
		db:    db,
		cache: expirable.NewLRU[findingPolicyFilterKey, findingPolicyFilters](findingPolicyFilterCacheSize, nil, findingPolicyFilterCacheTTL),
	}
}

// missing records policies not found during one batch, so a batch of findings for
// a deleted policy queries once rather than once per finding.
func (r *findingPolicyFilterResolver) filtersFor(ctx context.Context, message *riskv1.Finding, missing map[findingPolicyFilterKey]struct{}) (findingPolicyFilters, error) {
	if message == nil || r.db == nil {
		return emptyFindingPolicyFilters(), nil
	}

	projectID, err := uuid.Parse(message.GetProjectId())
	if err != nil {
		return emptyFindingPolicyFilters(), nil
	}
	policyID, err := uuid.Parse(message.GetRiskPolicyId())
	if err != nil {
		return emptyFindingPolicyFilters(), nil
	}

	key := findingPolicyFilterKey{projectID: projectID, policyID: policyID}
	if filters, ok := r.cache.Get(key); ok {
		return filters, nil
	}
	if _, ok := missing[key]; ok {
		return emptyFindingPolicyFilters(), nil
	}

	policy, err := repo.New(r.db).GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        policyID,
		ProjectID: projectID,
	})
	// Not cached across batches: on a read replica a missing row may be replication lag.
	if errors.Is(err, pgx.ErrNoRows) {
		missing[key] = struct{}{}
		return emptyFindingPolicyFilters(), nil
	}
	if err != nil {
		return emptyFindingPolicyFilters(), fmt.Errorf("get risk policy: %w", err)
	}

	filters := findingPolicyFilters{
		disabledRules:         risk_analysis.NewDisabledRuleSet(policy.DisabledRules),
		builtinPresetsEnabled: risk_analysis.BuiltinPresetsEnabledFromConfig(policy.AnalyzerConfig),
	}
	r.cache.Add(key, filters)
	return filters, nil
}
