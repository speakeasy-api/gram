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
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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

// resolveUserScope resolves an employee identifier into either a canonical
// map-backed identity (fold flag on) or the legacy Postgres-expanded set —
// exactly one of the two is populated, and the repo's identity filter prefers
// the canonical one when enabled.
func (s *Service) resolveUserScope(ctx context.Context, orgID, identifier string) (repo.UserIdentity, repo.CanonicalUserIdentity) {
	none := repo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""}
	if identifier == "" {
		return repo.UserIdentity{UserIDs: nil, Emails: nil}, none
	}
	if fold, _ := s.canonicalIdentityMode(ctx, orgID); fold {
		return repo.UserIdentity{UserIDs: nil, Emails: nil}, s.resolveCanonicalUserIdentity(ctx, orgID, identifier)
	}
	return s.resolveEmployeeIdentity(ctx, orgID, identifier), none
}

// resolveCanonicalUserIdentity builds the map-backed identity for one
// identifier. An email identifier needs no lookup at all — the owning user id
// and the folded email comparison both come from the identity_map inside the
// query. A user-id identifier keeps one directory lookup for the email leg;
// when it fails the scope still matches every user_id-keyed row, so a lookup
// failure degrades coverage rather than breaking the page.
func (s *Service) resolveCanonicalUserIdentity(ctx context.Context, orgID, identifier string) repo.CanonicalUserIdentity {
	ident := repo.CanonicalUserIdentity{OrgID: orgID, UserID: "", EmailLower: ""}
	if strings.Contains(identifier, "@") {
		ident.EmailLower = conv.NormalizeEmail(identifier)
		return ident
	}

	ident.UserID = identifier
	rows, err := usersRepo.New(s.db).GetConnectedUsersByIDs(ctx, usersRepo.GetConnectedUsersByIDsParams{
		Ids:            []string{identifier},
		OrganizationID: orgID,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "resolve canonical identity directory email", attr.SlogError(err))
	}
	if len(rows) == 1 {
		ident.EmailLower = conv.NormalizeEmail(rows[0].Email)
	}
	return ident
}
