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
	"slices"
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

// shadowCompareSearchUsersFold re-runs the employee list query with folding
// enabled and logs how the folded list diverges from the literal one already
// served: row counts, keys that exist only folded (canonical emails replacing
// literal variants), whether the shared keys changed relative order, and the
// input+output token delta — folding merges rows, so on an untruncated page
// the token sum must be preserved and the folded list can only shrink. Emails
// are deliberately not logged; counts carry the signal. Runs in the
// background off a detached context so it never delays or fails the caller's
// request.
func (s *Service) shadowCompareSearchUsersFold(ctx context.Context, orgID string, params repo.SearchUsersParams, literal []repo.UserSummary) {
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
		folded, err := s.chRepo.SearchUsers(bgCtx, params)
		if err != nil {
			s.logger.WarnContext(bgCtx, "identity fold shadow employee list query failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
			return
		}

		literalKeys := make(map[string]struct{}, len(literal))
		var literalTokens, foldedTokens int64
		for _, u := range literal {
			literalKeys[u.UserID] = struct{}{}
			literalTokens += u.TotalInputTokens + u.TotalOutputTokens
		}
		newKeys := 0
		for _, u := range folded {
			foldedTokens += u.TotalInputTokens + u.TotalOutputTokens
			if _, ok := literalKeys[u.UserID]; !ok {
				newKeys++
			}
		}

		// Ordering divergence is measured over the shared keys the fold did
		// NOT touch — same last-seen and same token sums in both lists. A
		// merged identity legitimately moves (its folded last-seen is the max
		// across its emails), so including merge targets would make this
		// signal permanently true for any org with real folds; untouched
		// groups have identical sort keys in both queries, and any reorder
		// among them is a genuine ordering bug, which is the divergence the
		// spec needs surfaced alongside membership and counts.
		type sortIdentity struct {
			lastSeen int64
			tokens   int64
		}
		literalSort := make(map[string]sortIdentity, len(literal))
		for _, u := range literal {
			literalSort[u.UserID] = sortIdentity{lastSeen: u.LastSeenUnixNano, tokens: u.TotalInputTokens + u.TotalOutputTokens}
		}
		untouched := make(map[string]struct{}, len(folded))
		for _, u := range folded {
			if lit, ok := literalSort[u.UserID]; ok && lit == (sortIdentity{lastSeen: u.LastSeenUnixNano, tokens: u.TotalInputTokens + u.TotalOutputTokens}) {
				untouched[u.UserID] = struct{}{}
			}
		}
		var untouchedLiteral, untouchedFolded []string
		for _, u := range literal {
			if _, ok := untouched[u.UserID]; ok {
				untouchedLiteral = append(untouchedLiteral, u.UserID)
			}
		}
		for _, u := range folded {
			if _, ok := untouched[u.UserID]; ok {
				untouchedFolded = append(untouchedFolded, u.UserID)
			}
		}
		orderChanged := !slices.Equal(untouchedLiteral, untouchedFolded)

		s.logger.InfoContext(bgCtx, "identity fold shadow employee list comparison",
			attr.SlogOrganizationID(orgID),
			attr.SlogIdentityFoldLiteralGroups(len(literal)),
			attr.SlogIdentityFoldCanonicalGroups(len(folded)),
			attr.SlogIdentityFoldNewKeys(newKeys),
			attr.SlogIdentityFoldOrderChanged(orderChanged),
			attr.SlogIdentityFoldTokenDelta(foldedTokens-literalTokens),
			// On a truncated page both lists are capped at limit+1 and the
			// deltas are page-cap artifacts, not divergence — folding frees
			// slots and pulls in groups past the literal horizon. Readers
			// deciding a rollout must weigh only truncated=false lines.
			attr.SlogIdentityFoldTruncated(len(literal) >= params.Limit),
		)
	}()
}

// CanonicalOrgFor exposes the fold gate to services outside this package that
// run their own identity-folded queries, so the rollout flag stays a single
// switch rather than one per caller.
func (s *Service) CanonicalOrgFor(ctx context.Context, orgID string) string {
	return s.canonicalOrgFor(ctx, orgID)
}

// canonicalOrgFor resolves the fold flag to the org id the repo params carry:
// the org id when folding is enabled, "" when the org serves literal
// identities. The single choke point for fold gating on org-scoped reads.
func (s *Service) canonicalOrgFor(ctx context.Context, orgID string) string {
	if fold, _ := s.canonicalIdentityMode(ctx, orgID); fold {
		return orgID
	}
	return ""
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
		if ident := s.resolveCanonicalUserIdentity(ctx, orgID, identifier); ident.Enabled() {
			return repo.UserIdentity{UserIDs: nil, Emails: nil}, ident
		}
		// The org id failed the SQL-literal allowlist, so the canonical filter
		// cannot be built for it. Degrade to the legacy expanded scope like
		// every other fold site does on an unfoldable org id — returning the
		// disabled identity alongside the empty legacy set would leave the
		// per-user queries with no user filter at all.
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
	// The identity map excludes deleted users, so a deleted directory row's
	// email may already belong to a different active owner in the map; folding
	// it would pull that owner's email-only rows onto this page. Degrade to
	// id-only scope instead, the same fallback as a lookup failure.
	if len(rows) == 1 && !rows[0].DeletedAt.Valid {
		ident.EmailLower = conv.NormalizeEmail(rows[0].Email)
	}
	return ident
}
