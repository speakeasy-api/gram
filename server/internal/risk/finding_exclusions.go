package risk

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

const (
	exclusionsSetCacheSize = 1000
	exclusionsSetCacheTTL  = time.Minute
)

type findingExclusionKey struct {
	projectID uuid.UUID
	policyID  uuid.UUID
}

// FindingExclusionResolver evaluates findings against the currently enabled
// project and global exclusions for their policy.
type FindingExclusionResolver struct {
	db    repo.DBTX
	cache *expirable.LRU[findingExclusionKey, risk_analysis.ExclusionSet]
}

func NewFindingExclusionResolver(db repo.DBTX) *FindingExclusionResolver {
	return &FindingExclusionResolver{
		db:    db,
		cache: expirable.NewLRU[findingExclusionKey, risk_analysis.ExclusionSet](exclusionsSetCacheSize, nil, exclusionsSetCacheTTL),
	}
}

// ExcludedBy returns the matching exclusion id. Lookup and identifier errors
// are returned so each caller can apply its own delivery policy.
func (r *FindingExclusionResolver) ExcludedBy(ctx context.Context, message *riskv1.Finding) (uuid.UUID, bool, error) {
	if message == nil {
		return uuid.UUID{}, false, errors.New("finding is nil")
	}

	projectID, err := uuid.Parse(message.GetProjectId())
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("parse finding project id: %w", err)
	}
	policyID, err := uuid.Parse(message.GetRiskPolicyId())
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("parse finding risk policy id: %w", err)
	}

	set, err := r.exclusionSetFor(ctx, projectID, policyID)
	if err != nil || set.Empty() {
		return uuid.UUID{}, false, err
	}

	// ExclusionSet.ExcludedBy matches on RuleID, Source and Match only; the
	// remaining fields are set for completeness (exhaustruct) but unused.
	exclusionID, excluded := set.ExcludedBy(scanners.Finding{
		RuleID:              message.GetRuleId(),
		Description:         message.GetDescription(),
		Match:               message.GetMatch(),
		StartPos:            int(message.GetStartPos()),
		EndPos:              int(message.GetEndPos()),
		Tags:                message.GetTags(),
		Source:              message.GetSource(),
		Confidence:          message.GetConfidence(),
		DeadLetterReason:    message.GetDeadLetterReason(),
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	})
	return exclusionID, excluded, nil
}

func (r *FindingExclusionResolver) exclusionSetFor(ctx context.Context, projectID, policyID uuid.UUID) (risk_analysis.ExclusionSet, error) {
	key := findingExclusionKey{projectID: projectID, policyID: policyID}
	if set, ok := r.cache.Get(key); ok {
		return set, nil
	}
	if r.db == nil {
		return risk_analysis.ExclusionSet{}, errors.New("finding exclusion database is unavailable")
	}

	exclusions, err := repo.New(r.db).ListEnabledExclusionsForPolicy(ctx, repo.ListEnabledExclusionsForPolicyParams{
		ProjectID:    projectID,
		RiskPolicyID: uuid.NullUUID{UUID: policyID, Valid: true},
	})
	if err != nil {
		return risk_analysis.ExclusionSet{}, fmt.Errorf("list exclusions for policy: %w", err)
	}

	set := risk_analysis.NewExclusionSet(exclusions)
	r.cache.Add(key, set)
	return set, nil
}
