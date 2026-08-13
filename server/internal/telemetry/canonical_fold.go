package telemetry

// Rollout plumbing for canonical identity folding in the cost analytics
// endpoints. Two PostHog flags gate the ClickHouse identity_map fold: the
// fold flag switches email filters and group-bys onto canonical employee
// identities, and the shadow flag runs the folded variant of the Query table
// read alongside the literal one — serving the literal result — and logs how
// the two diverge, so the fold can be validated on real traffic before any
// org sees it.

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// shadowCompareTimeout bounds the background folded re-query so a slow shadow
// read cannot pile up goroutines behind a degraded ClickHouse.
const shadowCompareTimeout = 30 * time.Second

// maxConcurrentShadowCompares caps in-flight shadow re-queries across all
// requests. Shadow is sampling, not accounting: when ClickHouse slows down and
// the cap is hit, further comparisons are skipped rather than queued so
// validation traffic cannot amplify the slowdown.
const maxConcurrentShadowCompares = 4

// queryTouchesEmail reports whether a request involves the email dimension at
// all — the only case where folding changes anything, and the gate on paying
// the flag-evaluation and shadow-compare cost.
func queryTouchesEmail(groupBy string, filters []repo.AttributeMetricsFilter) bool {
	if groupBy == "email" {
		return true
	}
	for _, f := range filters {
		if f.Dimension == "email" && len(f.Values) > 0 {
			return true
		}
	}
	return false
}

// canonicalIdentityMode resolves the rollout state for an organization: fold
// serves canonical identities, shadow logs divergence while serving literal
// results. Fold wins over shadow. Flag evaluation follows the budgets
// pattern: PostHog organization-group targeting by org slug, degrading to
// distinct-id-only evaluation when the slug lookup fails, and failing closed
// (literal behavior) on flag errors.
func (s *Service) canonicalIdentityMode(ctx context.Context, orgID string) (fold, shadow bool) {
	if s.featureFlags == nil || orgID == "" {
		return false, false
	}

	var groups map[string]string
	org, err := s.orgsRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve organization slug for identity fold flag", attr.SlogError(err))
	} else {
		groups = feature.OrgProjectGroups(org.Slug, "")
	}

	fold, err = s.featureFlags.IsFlagEnabled(ctx, feature.FlagCanonicalIdentityFold, orgID, groups)
	if err != nil {
		s.logger.WarnContext(ctx, "identity fold flag check failed; serving literal identities", attr.SlogError(err))
		return false, false
	}
	if fold {
		return true, false
	}

	shadow, err = s.featureFlags.IsFlagEnabled(ctx, feature.FlagCanonicalIdentityFoldShadow, orgID, groups)
	if err != nil {
		s.logger.WarnContext(ctx, "identity fold shadow flag check failed", attr.SlogError(err))
		return false, false
	}
	return false, shadow
}

// shadowCompareCanonicalFold re-runs the aggregate table query with folding
// enabled and logs how it diverges from the literal rows already served. The
// comparison is intentionally coarse — group count and summed total cost —
// which is exactly what folding changes: identities collapsing into fewer
// groups must preserve the cost total. Runs in the background off a detached
// context so it never delays or fails the caller's request.
func (s *Service) shadowCompareCanonicalFold(ctx context.Context, orgID string, params repo.AttributeMetricsQueryParams, literalRows []repo.AttributeMetricsRow) {
	select {
	case s.shadowFoldSem <- struct{}{}:
	default:
		s.logger.InfoContext(ctx, "identity fold shadow comparison skipped: concurrency cap reached", attr.SlogOrganizationID(orgID))
		return
	}

	bgCtx := context.WithoutCancel(ctx)
	go func() {
		defer func() { <-s.shadowFoldSem }()
		bgCtx, cancel := context.WithTimeout(bgCtx, shadowCompareTimeout)
		defer cancel()

		params.CanonicalIdentityOrg = orgID
		foldedRows, err := s.chRepo.QueryAttributeMetricsTable(bgCtx, params)
		if err != nil {
			s.logger.WarnContext(bgCtx, "identity fold shadow query failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
			return
		}

		literalCost, foldedCost := 0.0, 0.0
		for _, row := range literalRows {
			literalCost += row.TotalCost
		}
		for _, row := range foldedRows {
			foldedCost += row.TotalCost
		}

		s.logger.InfoContext(bgCtx, "identity fold shadow comparison",
			attr.SlogOrganizationID(orgID),
			attr.SlogIdentityFoldLiteralGroups(len(literalRows)),
			attr.SlogIdentityFoldCanonicalGroups(len(foldedRows)),
			attr.SlogIdentityFoldCostDelta(foldedCost-literalCost),
		)
	}()
}
