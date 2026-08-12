package remotesessions_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// The duplicate preflight is the manual-path counterpart to the resolve-by-URL
// lookup automatic setup performs: it answers "does anything I can see already
// describe this upstream?" for an operator filling in a form by hand. Every
// assertion below is about what a tier can SEE, because that is the only thing
// the three tiers do differently.

func projectDuplicatePreflight(issuerURL string) *gen.GetRemoteSessionIssuerDuplicatePreflightPayload {
	return &gen.GetRemoteSessionIssuerDuplicatePreflightPayload{
		Issuer:           &issuerURL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

func organizationDuplicatePreflight(issuerURL string) *orgissuersgen.GetIssuerDuplicatePreflightPayload {
	return &orgissuersgen.GetIssuerDuplicatePreflightPayload{
		Issuer:       &issuerURL,
		SessionToken: nil,
		ApikeyToken:  nil,
	}
}

func globalDuplicatePreflight(issuerURL string) *adminrsgen.GetGlobalIssuerDuplicatePreflightPayload {
	return &adminrsgen.GetGlobalIssuerDuplicatePreflightPayload{
		Issuer:       &issuerURL,
		SessionToken: nil,
	}
}

// matchIDs reduces a preflight result to the ids it named, in order.
func matchIDs(matches []*types.RemoteSessionIssuerDuplicateMatch) []string {
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.ID)
	}
	return ids
}

// --- Project tier ---

// The empty result is the good case: nothing describes this URL, so creating
// one is not a duplicate and no warning should fire.
func TestGetRemoteSessionIssuerDuplicatePreflight_NoMatchesForUnknownURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight("https://nothing-here.example.com"))
	require.NoError(t, err)
	require.Empty(t, result.Matches)
}

func TestGetRemoteSessionIssuerDuplicatePreflight_MatchesOwnProjectIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://own-project.example.com"
	id := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "own-project", issuerURL)

	result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	require.Equal(t, id.String(), result.Matches[0].ID)
	require.Equal(t, "project-specific", result.Matches[0].Tier)
	require.Equal(t, issuerURL, result.Matches[0].Issuer)
	// A project caller is already inside the only project its matches can belong
	// to, so naming it would be noise.
	require.Empty(t, result.Matches[0].ProjectName)
}

// Both inherited tiers count as duplicates: an organization-level or platform
// record describing this upstream is one the project could attach to instead of
// adding its own.
func TestGetRemoteSessionIssuerDuplicatePreflight_MatchesInheritedTiers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://inherited.example.com"
	orgIssuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "inherited-org", issuerURL)
	platformIssuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "inherited-platform", issuerURL)

	result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Equal(t, []string{orgIssuerID.String(), platformIssuerID.String()}, matchIDs(result.Matches))
	require.Equal(t, "organization-level", result.Matches[0].Tier)
	require.Equal(t, "platform-level", result.Matches[1].Tier)
}

// Matches arrive in resolution order, narrowest tier first, so the first entry
// is the record this project resolves the URL to today.
func TestGetRemoteSessionIssuerDuplicatePreflight_RanksNarrowestTierFirst(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://ranked.example.com"
	// Seeded broadest-first so a result that merely preserved insertion order
	// would fail this.
	platformIssuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "ranked-platform", issuerURL)
	orgIssuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "ranked-org", issuerURL)
	projectIssuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "ranked-project", issuerURL)

	result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Equal(t,
		[]string{projectIssuerID.String(), orgIssuerID.String(), platformIssuerID.String()},
		matchIDs(result.Matches),
	)
}

// The oldest record within a tier wins, matching how the resolver breaks a tie
// among several same-tier records on one URL. Several project-tier rows on one
// URL are normal, since issuer creation is unconditional by design.
func TestGetRemoteSessionIssuerDuplicatePreflight_OldestFirstWithinATier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://tiebreak.example.com"
	firstID := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "tiebreak-first", issuerURL)
	secondID := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "tiebreak-second", issuerURL)

	result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Equal(t, []string{firstID.String(), secondID.String()}, matchIDs(result.Matches))
}

// The acceptance criterion that keeps the preflight honest: it must agree with
// the resolver about which record wins, rather than restating precedence in a
// second place where the two could drift. Ordering the response in resolution
// order is what makes this expressible as an equality instead of a
// reimplementation.
func TestGetRemoteSessionIssuerDuplicatePreflight_TopMatchAgreesWithResolver(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://parity.example.com"
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "parity-platform", issuerURL)
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "parity-org", issuerURL)
	seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "parity-project", issuerURL)

	// Every spelling the shared candidate set collapses, so the two paths are
	// compared on canonicalization as well as on precedence.
	for _, spelling := range []string{
		issuerURL,
		issuerURL + "/",
		"https://PARITY.example.com",
		"https://parity.example.com:443",
	} {
		resolved, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL(spelling))
		require.NoError(t, err, "resolver should find a match for %q", spelling)

		preflight, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(spelling))
		require.NoError(t, err)
		require.NotEmpty(t, preflight.Matches, "preflight should find matches for %q", spelling)
		require.Equal(t, resolved.ID, preflight.Matches[0].ID,
			"preflight's top match must be the record the resolver returns for %q", spelling)
	}
}

// Canonicalization applies to the supplied URL only, never to stored values, so
// these are the spellings a stored record can be found by.
func TestGetRemoteSessionIssuerDuplicatePreflight_CollapsesEquivalentSpellings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://spelling.example.com"
	id := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "spelling", issuerURL)

	for _, spelling := range []string{
		issuerURL,
		issuerURL + "/",
		"https://spelling.example.com:443",
		"  https://spelling.example.com  ",
	} {
		result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(spelling))
		require.NoError(t, err)
		require.Len(t, result.Matches, 1, "spelling %q should match the stored record", spelling)
		require.Equal(t, id.String(), result.Matches[0].ID)
	}
}

// http and https are deliberately distinct: same host, different security
// properties, and an upstream reachable over both is a misconfiguration Gram
// should not paper over.
func TestGetRemoteSessionIssuerDuplicatePreflight_DoesNotEquateSchemes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "scheme", "https://scheme-split.example.com")

	result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight("http://scheme-split.example.com"))
	require.NoError(t, err)
	require.Empty(t, result.Matches)
}

// A partially typed URL is the normal state of a form field, so an unusable
// issuer identifier is "nothing matched", not an error. Failing here would emit
// a client-fault 4xx — and an errored trace span — for every keystroke.
func TestGetRemoteSessionIssuerDuplicatePreflight_UnusableURLReturnsNoMatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	for _, unusable := range []string{
		"",
		"   ",
		"h",
		"https://",
		"not a url at all",
		"ftp://wrong-scheme.example.com",
		"https://user:pass@userinfo.example.com",
		"https://query.example.com?a=b",
		"https://fragment.example.com#section",
	} {
		result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(unusable))
		require.NoError(t, err, "unusable url %q must not error", unusable)
		require.Empty(t, result.Matches, "unusable url %q must match nothing", unusable)
		require.NotNil(t, result.Matches, "matches must serialize as [] rather than null")
	}
}

// The issuer parameter is deliberately not Required in the design, because Goa
// cannot tell an absent query parameter from an empty one and requiring it would
// turn a blank form field into a 400 at the transport decoder. An omitted
// parameter therefore has to reach the handler and answer like any other
// unusable URL.
func TestGetRemoteSessionIssuerDuplicatePreflight_OmittedIssuerReturnsNoMatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, &gen.GetRemoteSessionIssuerDuplicatePreflightPayload{
		Issuer:           nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Matches)
	require.NotNil(t, result.Matches)
}

// A sibling project's records must never surface here. The project arm is
// scoped to the caller's own project for exactly this reason, and widening it
// would turn the preflight into an existence oracle over projects the caller
// may hold no grant on.
func TestGetRemoteSessionIssuerDuplicatePreflight_IgnoresSiblingProjectIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://sibling.example.com"
	siblingProjectID := createProject(t, ctx, ti.conn, "sibling-project")
	seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(siblingProjectID), conv.ToPGText(orgID), "sibling-issuer", issuerURL)

	result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Empty(t, result.Matches)
}

// The whole point of warning rather than validating: duplicating an issuer URL
// is legitimate, so a create that the preflight flagged still succeeds.
func TestGetRemoteSessionIssuerDuplicatePreflight_DoesNotBlockCreate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	const issuerURL = "https://idp.example.com"
	platformIssuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "nonblocking-platform", issuerURL)

	warned, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Len(t, warned.Matches, 1, "the create below must be one the preflight warned about")
	require.Equal(t, platformIssuerID.String(), warned.Matches[0].ID)

	// newIssuerPayload carries the same issuer URL as the platform record above.
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("duplicates-platform"))
	require.NoError(t, err, "a flagged duplicate must still be creatable")
	require.Equal(t, issuerURL, created.Issuer)
	require.NotEqual(t, platformIssuerID.String(), created.ID)

	// And the new record now shows up as a duplicate itself, ranked ahead of the
	// platform one because a project record is what this project would resolve.
	after, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Equal(t, []string{created.ID, platformIssuerID.String()}, matchIDs(after.Matches))
}

// Truncation keeps a response bounded even though tenants control how many
// records can accumulate on one URL.
func TestGetRemoteSessionIssuerDuplicatePreflight_TruncatesToCap(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://many.example.com"
	const seeded = 15
	for i := range seeded {
		seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), fmt.Sprintf("many-%d", i), issuerURL)
	}

	result, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	// Asserted exactly, not as "fewer than seeded": a looser bound stays green
	// if the cap regresses to a flat total or to one per tier.
	require.Len(t, result.Matches, 3, "one tier present, so the cap is the per-tier cap")
	require.Less(t, len(result.Matches), seeded)
}

// The platform tier is single-tier, so its ordering is the whole order and
// nothing else exercises it.
func TestGetGlobalIssuerDuplicatePreflight_OldestFirstAndTruncated(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	const issuerURL = "https://catalog-many.example.com"
	ids := make([]string, 0, 5)
	for i := range 5 {
		id := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, fmt.Sprintf("catalog-many-%d", i), issuerURL)
		ids = append(ids, id.String())
	}

	result, err := ti.service.GetGlobalIssuerDuplicatePreflight(adminCtx, globalDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Equal(t, ids[:3], matchIDs(result.Matches), "oldest three, in creation order")
}

// Truncation must never cost a whole tier. A project holding many records on
// one URL is the case that makes this bite: a flat cap over a narrowest-first
// ranking would spend the entire budget on project rows and drop the
// organization and platform matches, which are the two most useful things the
// warning has to say. Both preflights that can see more than one tier are
// checked, because they bound the result in different places — the project tier
// truncates in Go over an unbounded query, the organization tier carries a SQL
// LIMIT as well.
func TestIssuerDuplicatePreflight_TruncationKeepsEveryTier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://crowded.example.com"
	// Seeded first, so an ordering that only respected age would rank these
	// ahead of everything below and a flat cap would keep nothing else.
	for i := range 15 {
		seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), fmt.Sprintf("crowded-%d", i), issuerURL)
	}
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "crowded-org", issuerURL)
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "crowded-platform", issuerURL)

	projectResult, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Contains(t, tierBySlug(projectResult.Matches), "crowded-org")
	require.Contains(t, tierBySlug(projectResult.Matches), "crowded-platform")

	orgResult, err := ti.service.GetIssuerDuplicatePreflight(ctx, organizationDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Contains(t, tierBySlug(orgResult.Matches), "crowded-org")
	require.Contains(t, tierBySlug(orgResult.Matches), "crowded-platform")

	// And the narrowest tier still leads, so matches[0] remains the record the
	// caller resolves this URL to.
	require.Equal(t, "project-specific", projectResult.Matches[0].Tier)
}

// The mirror of the test above, crowding the BROADEST tier instead. This is the
// case a single shared SQL budget gets wrong in the opposite direction: enough
// global rows to fill the limit before any tenant row is read, leaving the
// organization administrator's own duplicates invisible. Per-tier limits are
// what make the direction of the crowding irrelevant.
func TestGetIssuerDuplicatePreflight_TruncationSurvivesACrowdedGlobalTier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://crowded-catalog.example.com"
	for i := range 12 {
		seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, fmt.Sprintf("crowded-catalog-%d", i), issuerURL)
	}
	orgIssuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "crowded-catalog-org", issuerURL)

	result, err := ti.service.GetIssuerDuplicatePreflight(ctx, organizationDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Contains(t, matchIDs(result.Matches), orgIssuerID.String(),
		"the organization's own duplicate must survive a crowded platform catalog")
	require.Equal(t, orgIssuerID.String(), result.Matches[0].ID,
		"and it still leads, being the narrower tier")
	require.Len(t, result.Matches, 4, "3 platform + 1 organization")
}

func TestGetRemoteSessionIssuerDuplicatePreflight_RequiresProjectRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight("https://denied.example.com"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// --- Organization tier ---

// The reason the org tier gets its own query rather than reusing the project
// one: an administrator adding an organization-level issuer most needs to know
// which of their projects already configured the same URL separately, and the
// project tier's organization arm cannot see project-scoped records at all.
func TestGetIssuerDuplicatePreflight_ReportsProjectScopedDuplicates(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://org-sees-projects.example.com"
	otherProjectID := createProject(t, ctx, ti.conn, "org-preflight-project")
	projectIssuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(otherProjectID), conv.ToPGText(orgID), "org-sees-project-issuer", issuerURL)

	result, err := ti.service.GetIssuerDuplicatePreflight(ctx, organizationDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	require.Equal(t, projectIssuerID.String(), result.Matches[0].ID)
	require.Equal(t, "project-specific", result.Matches[0].Tier)
	// The owning project is named here, unlike at the project tier, because an
	// org administrator otherwise cannot place the match.
	require.NotEmpty(t, result.Matches[0].ProjectName)
}

func TestGetIssuerDuplicatePreflight_IncludesOrganizationAndPlatformTiers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://org-all-tiers.example.com"
	orgIssuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "org-all-tiers-org", issuerURL)
	platformIssuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "org-all-tiers-platform", issuerURL)

	result, err := ti.service.GetIssuerDuplicatePreflight(ctx, organizationDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Equal(t, []string{orgIssuerID.String(), platformIssuerID.String()}, matchIDs(result.Matches))
	require.Empty(t, result.Matches[0].ProjectName, "an organization-level match has no owning project")
}

func TestGetIssuerDuplicatePreflight_IgnoresOtherOrganizations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	const issuerURL = "https://foreign-org.example.com"
	foreignOrgID := createOrganization(t, ctx, ti.conn, "foreign-org")
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(foreignOrgID), "foreign-org-issuer", issuerURL)

	result, err := ti.service.GetIssuerDuplicatePreflight(ctx, organizationDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Empty(t, result.Matches)
}

func TestGetIssuerDuplicatePreflight_UnusableURLReturnsNoMatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.GetIssuerDuplicatePreflight(ctx, organizationDuplicatePreflight("https://bad.example.com?query=1"))
	require.NoError(t, err)
	require.Empty(t, result.Matches)
}

func TestGetIssuerDuplicatePreflight_RequiresOrgRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetIssuerDuplicatePreflight(ctx, organizationDuplicatePreflight("https://denied.example.com"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// --- Platform tier ---

func TestGetGlobalIssuerDuplicatePreflight_MatchesGlobalIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	const issuerURL = "https://catalog.example.com"
	id := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "catalog-entry", issuerURL)

	result, err := ti.service.GetGlobalIssuerDuplicatePreflight(adminCtx, globalDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	require.Equal(t, id.String(), result.Matches[0].ID)
	require.Equal(t, "platform-level", result.Matches[0].Tier)
}

// The leak regression test. The platform preflight reads the global partition
// only: tenant records naming the same upstream belong to the convergence
// surface, and surfacing them here would put one organization's configuration
// in front of a form that is only asking about the shared catalog.
//
// This is also what guards the reason the platform tier has its own query
// instead of reusing the three-arm one with a NULL project_id. If that
// predicate is ever rewritten in a way that makes `project_id = NULL` match,
// this test is what fails.
func TestGetGlobalIssuerDuplicatePreflight_IgnoresTenantIssuersOnSameURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://tenant-owned.example.com"
	seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "tenant-project-row", issuerURL)
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "tenant-org-row", issuerURL)

	result, err := ti.service.GetGlobalIssuerDuplicatePreflight(adminCtx, globalDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Empty(t, result.Matches, "tenant issuers must never surface in the platform preflight")
}

func TestGetGlobalIssuerDuplicatePreflight_UnusableURLReturnsNoMatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	result, err := ti.service.GetGlobalIssuerDuplicatePreflight(adminCtx, globalDuplicatePreflight("nonsense"))
	require.NoError(t, err)
	require.Empty(t, result.Matches)
}

func TestGetGlobalIssuerDuplicatePreflight_RequiresPlatformAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetGlobalIssuerDuplicatePreflight(ctx, globalDuplicatePreflight("https://catalog.example.com"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// Guards the invariant that lets every tier share one ranking and projection:
// the tier label a match carries is derived from its tenancy columns, not from
// which endpoint answered.
func TestIssuerDuplicatePreflight_TierLabelsMatchTenancyColumns(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://labels.example.com"
	seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "labels-project", issuerURL)
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "labels-org", issuerURL)
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "labels-platform", issuerURL)

	expected := map[string]string{
		"labels-project":  "project-specific",
		"labels-org":      "organization-level",
		"labels-platform": "platform-level",
	}

	projectResult, err := ti.service.GetRemoteSessionIssuerDuplicatePreflight(ctx, projectDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Equal(t, expected, tierBySlug(projectResult.Matches))

	orgResult, err := ti.service.GetIssuerDuplicatePreflight(ctx, organizationDuplicatePreflight(issuerURL))
	require.NoError(t, err)
	require.Equal(t, expected, tierBySlug(orgResult.Matches))
}

func tierBySlug(matches []*types.RemoteSessionIssuerDuplicateMatch) map[string]string {
	out := make(map[string]string, len(matches))
	for _, match := range matches {
		out[match.Slug] = match.Tier
	}
	return out
}
