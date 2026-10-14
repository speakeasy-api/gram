package risk

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk/policyflags"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// enforcementPlan is everything a realtime scan resolves before it reads the
// content: the enforcing policies that apply to the principal, each with its
// compiled detection scopes and exclusions, and the flags that select the
// engines. It depends only on the tenant and principal, so scans of many
// inputs for one request can share it.
type enforcementPlan struct {
	// enforcingCount is the number of enabled enforcing policies in the
	// project before audience filtering. Zero takes the "skipped" fast path.
	enforcingCount int

	// policies are the enforcing policies whose audience includes the principal.
	policies []plannedPolicy

	promptPoliciesOn bool
	mode             feature.Variant
	orgSlug          string

	// legacyLanes routes the gitleaks and presidio sources through their
	// Pub/Sub enforcement lanes. It is always false in the llm engine mode,
	// whose single lane replaces them.
	legacyLanes bool

	// retry reports a policy whose state failed to load for a reason the
	// next scan may not repeat, so the plan is used once and not memoized.
	retry bool
}

// plannedPolicy pairs a policy with the per-policy state its scan reads.
// A failure to build that state is kept, rather than failing the plan, so
// that one broken policy is reported (and marks the scan incomplete) each
// time it is scanned while the other policies still enforce.
type plannedPolicy struct {
	policy        repo.RiskPolicy
	categoryScope ra.CategoryScope
	scopeErr      error
	exclusions    ra.ExclusionSet
	exclusionsErr error
}

func (p *enforcementPlan) applicablePolicies() []repo.RiskPolicy {
	policies := make([]repo.RiskPolicy, len(p.policies))
	for i, planned := range p.policies {
		policies[i] = planned.policy
	}
	return policies
}

type enforcementPlanMemoKey struct{}

type enforcementPlanMemo struct {
	mu    sync.Mutex
	plans map[string]*enforcementPlanEntry
}

type enforcementPlanEntry struct {
	done chan struct{}
	plan *enforcementPlan
	err  error
}

// WithEnforcementPlanMemo makes every realtime scan under ctx resolve its
// enforcement plan once per tenant and principal. Without it, each scan
// lists the enforcing policies, resolves the principal's grants, loads each
// policy's exclusions and compiles its detection scopes again, which
// dominates a transcript that is scanned as many inputs on a fixed budget.
// The plan is a snapshot: policy changes take effect on the next request.
func WithEnforcementPlanMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, enforcementPlanMemoKey{}, &enforcementPlanMemo{mu: sync.Mutex{}, plans: map[string]*enforcementPlanEntry{}})
}

// enforcementPlanFor returns the memoized plan for the request's principal
// when ctx carries a memo, resolving it on first use. Concurrent scans that
// miss the same plan wait for one resolution. A failed resolution, or a plan
// with a retryable policy failure, is not remembered: its waiters receive the
// same result and the next scan resolves again.
func (s *Scanner) enforcementPlanFor(ctx context.Context, provenance metering.RiskProvenance) (*enforcementPlan, error) {
	memo, _ := ctx.Value(enforcementPlanMemoKey{}).(*enforcementPlanMemo)
	if memo == nil {
		return s.resolveEnforcementPlan(ctx, provenance.OrganizationID, provenance.ProjectID, provenance.UserID)
	}

	key := enforcementPlanKey(ctx, provenance)
	memo.mu.Lock()
	entry, ok := memo.plans[key]
	if !ok {
		entry = &enforcementPlanEntry{done: make(chan struct{}), plan: nil, err: nil}
		memo.plans[key] = entry
		memo.mu.Unlock()

		entry.plan, entry.err = s.resolveEnforcementPlan(ctx, provenance.OrganizationID, provenance.ProjectID, provenance.UserID)
		if entry.err != nil || entry.plan.retry {
			memo.mu.Lock()
			delete(memo.plans, key)
			memo.mu.Unlock()
		}
		close(entry.done)
		return entry.plan, entry.err
	}
	memo.mu.Unlock()

	select {
	case <-entry.done:
		return entry.plan, entry.err
	case <-ctx.Done():
		return nil, fmt.Errorf("await enforcement plan: %w", context.Cause(ctx))
	}
}

// enforcementPlanKey identifies the principal whose grants select the plan's
// policies: an authenticated agent actor is evaluated as itself, otherwise
// the scanned user (see riskPolicyGrants).
func enforcementPlanKey(ctx context.Context, provenance metering.RiskProvenance) string {
	principal := "user:" + provenance.UserID
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeAgent {
		principal = "agent:" + actor.String()
	}
	return strings.Join([]string{provenance.OrganizationID, provenance.ProjectID.String(), principal}, "\x00")
}

func (s *Scanner) resolveEnforcementPlan(ctx context.Context, organizationID string, projectID uuid.UUID, userID string) (*enforcementPlan, error) {
	policies, err := s.repo.ListEnabledEnforcingPoliciesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list enforcing policies: %w", err)
	}
	plan := &enforcementPlan{
		enforcingCount:   len(policies),
		policies:         nil,
		promptPoliciesOn: false,
		mode:             feature.VariantRiskLLMOff,
		orgSlug:          "",
		legacyLanes:      false,
		retry:            false,
	}
	if len(policies) == 0 {
		return plan, nil
	}

	grants, err := s.riskPolicyGrants(ctx, organizationID, userID)
	if err != nil {
		return nil, err
	}

	applicable := make([]repo.RiskPolicy, 0, len(policies))
	for _, p := range policies {
		policyApplication, err := authz.RiskPolicyApplies(p.ID.String(), authz.RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""}).Evaluate(grants)
		if err != nil {
			return nil, fmt.Errorf("evaluate risk policy application: %w", err)
		}
		if !policyApplication.Satisfied {
			continue
		}
		applicable = append(applicable, p)
	}

	plan.policies = make([]plannedPolicy, len(applicable))
	var planning sync.WaitGroup
	for i, p := range applicable {
		planning.Go(func() { plan.policies[i] = s.planPolicy(ctx, p) })
	}
	planning.Wait()
	plan.retry = slices.ContainsFunc(plan.policies, func(p plannedPolicy) bool { return p.exclusionsErr != nil })

	// Each flag is resolved only under the condition in which a scan reads it.
	if slices.ContainsFunc(applicable, func(p repo.RiskPolicy) bool {
		return p.PolicyType == ra.PolicyTypePromptBased
	}) {
		// All enforcing policies for a project belong to the same org.
		plan.promptPoliciesOn = s.projectFlagEnabled(ctx, policies[0].OrganizationID, projectID, feature.FlagPromptPolicies)
	}

	// The risk engine mode selects the engine behind gitleaks, presidio and
	// prompt_injection sources for the whole org. The slug rides along for
	// the LLM lane's telemetry.
	if slices.ContainsFunc(applicable, func(p repo.RiskPolicy) bool {
		return llmanalyzer.CoversAnySource(p.Sources)
	}) {
		plan.mode, plan.orgSlug = policyflags.ProjectFlagMode(ctx, s.logger, s.repo, s.flags, organizationID, projectID, feature.FlagRiskLLMAnalyzer)
	}

	if plan.mode != feature.VariantRiskLLMLLM && len(applicable) > 0 {
		plan.legacyLanes = s.projectFlagEnabled(ctx, organizationID, projectID, feature.FlagRiskEnforcementPubsub)
	}
	return plan, nil
}

func (s *Scanner) planPolicy(ctx context.Context, policy repo.RiskPolicy) plannedPolicy {
	planned := plannedPolicy{
		policy:        policy,
		categoryScope: ra.CategoryScope{},
		scopeErr:      nil,
		exclusions:    ra.ExclusionSet{},
		exclusionsErr: nil,
	}

	// Per-category detection scopes are the only scoping surface.
	specified, err := ra.CompileDetectionScopes(s.celEng, ra.DetectionScopesFromConfig(policy.AnalyzerConfig))
	if err != nil {
		planned.scopeErr = fmt.Errorf("compile detection scopes: %w", err)
		return planned
	}
	planned.categoryScope = ra.NewCategoryScope(s.recommended, specified)

	// Prompt-based policies are judged without findings to exclude.
	if policy.PolicyType == ra.PolicyTypePromptBased {
		return planned
	}

	// Suppress findings matched by an exclusion (the policy's own plus any
	// global ones) so a false positive never blocks in real time.
	exclusionRows, err := s.repo.ListEnabledExclusionsForPolicy(ctx, repo.ListEnabledExclusionsForPolicyParams{
		ProjectID:    policy.ProjectID,
		RiskPolicyID: uuid.NullUUID{UUID: policy.ID, Valid: true},
	})
	if err != nil {
		planned.exclusionsErr = fmt.Errorf("list exclusions: %w", err)
		return planned
	}
	planned.exclusions = ra.NewExclusionSet(exclusionRows)
	return planned
}
