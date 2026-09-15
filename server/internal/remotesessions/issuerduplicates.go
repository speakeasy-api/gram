package remotesessions

import (
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Duplicate-issuer preflight (AIS-336). Each of the three create/edit surfaces
// asks, before it writes, whether some issuer it can already see describes the
// upstream authorization server the operator just typed.
//
// It is advisory in the strongest sense: nothing here blocks a write, nothing
// locks, and a create that races another create is expected to produce two
// records. Duplicating an issuer URL is a supported thing to want — a tenant may
// keep its own record precisely so it can attach different documentation,
// branding, scopes or audience handling — which is why remote_session_issuers
// has no uniqueness constraint on `issuer` and why the index on that column is
// not unique either. The goal is to stop accidental duplication, not to prevent
// deliberate duplication.
//
// The three tiers differ only in which records they can see. Everything after
// the query — canonicalization, ranking, projection, truncation — is shared, so
// a warning can never fire on different rules than the resolver it is supposed
// to agree with.

// maxIssuerDuplicateMatchesPerTier caps how many matches each tenancy tier
// contributes.
//
// A warning has to establish that duplicates exist and name a few; it does not
// have to enumerate them. Some cap is necessary because tenants control this row
// count — issuer creation is unconditional by design, so nothing stops one
// project from holding many records on a single URL — and this runs from a form,
// where the response lands in a client-side cache.
//
// The cap is PER TIER rather than over the whole list because the tiers are not
// interchangeable. A flat cap applied to a list ranked narrowest-first would let
// a project with many records on one URL push the organization-level and
// platform matches off the end, discarding the two most useful things the
// warning can say in order to keep a redundant project row. Per-tier truncation
// guarantees every tier that matched at all is represented.
const maxIssuerDuplicateMatchesPerTier = 3

// issuerDuplicateCandidate is one matching record, flattened to the fields the
// warning needs.
//
// Flattened rather than carrying a repo.RemoteSessionIssuer because the three
// tiers no longer produce one row type: the organization query selects a narrow
// column list (it has no reason to drag four array columns and a dozen text
// columns across for an advisory warning), while the project tier reuses the
// resolver's own query and so gets whole records.
type issuerDuplicateCandidate struct {
	// id is the matching remote_session_issuer id.
	id uuid.UUID

	// slug is the matching issuer's slug.
	slug string

	// name is the matching issuer's display name, empty when it has none.
	name string

	// issuerURL is the match's stored upstream URL, which may be spelled
	// differently from the URL that was looked up: canonicalization applies to
	// the supplied URL only.
	issuerURL string

	// tier is the tenancy tier that owns the match, and decides both ranking and
	// the label the warning shows.
	tier issuerScope

	// projectName names the owning project for a project-specific match, and is
	// empty otherwise. Only the organization tier populates it: a project-scoped
	// caller is already inside the single project its matches can belong to, and
	// every platform-tier match belongs to no project at all.
	projectName string
}

// issuerDuplicateCandidateFromRecord flattens a whole issuer record. Used by the
// tiers that read full rows: the project tier, which reuses the resolver's
// query, and the platform tier.
func issuerDuplicateCandidateFromRecord(record repo.RemoteSessionIssuer) issuerDuplicateCandidate {
	return issuerDuplicateCandidate{
		id:          record.ID,
		slug:        record.Slug,
		name:        conv.FromPGTextOrEmpty[string](record.Name),
		issuerURL:   record.Issuer,
		tier:        scopeOf(record),
		projectName: "",
	}
}

// buildIssuerDuplicatePreflight ranks, projects and truncates a tier's raw
// candidates into the shared wire result.
//
// Ranking is by tenancy tier, narrowest first, then oldest within a tier —
// the same precedence resolveIssuerByPrecedence applies, so matches[0] is the
// record the caller would resolve this URL to today. That correspondence is the
// point: it lets the preflight be checked against the resolver in a test
// instead of restating precedence in a second place where the two could drift.
//
// Candidates must arrive oldest-first within a tier, which every feeding query
// guarantees. The sort below is stable, so it reorders tiers without disturbing
// that.
//
// The per-tier cap is applied here as well as in the SQL that can express it,
// and the redundancy is deliberate: the project tier reuses the resolver's
// unbounded query, so this is the only thing bounding that path.
func buildIssuerDuplicatePreflight(candidates []issuerDuplicateCandidate) *types.RemoteSessionIssuerDuplicatePreflight {
	ranked := slices.Clone(candidates)
	slices.SortStableFunc(ranked, func(a, b issuerDuplicateCandidate) int {
		return int(a.tier) - int(b.tier)
	})

	perTier := make(map[issuerScope]int, 3)
	matches := make([]*types.RemoteSessionIssuerDuplicateMatch, 0, len(ranked))
	for _, candidate := range ranked {
		if perTier[candidate.tier] >= maxIssuerDuplicateMatchesPerTier {
			continue
		}
		perTier[candidate.tier]++

		matches = append(matches, &types.RemoteSessionIssuerDuplicateMatch{
			ID:          candidate.id.String(),
			Slug:        candidate.slug,
			Name:        candidate.name,
			Issuer:      candidate.issuerURL,
			Tier:        candidate.tier.String(),
			ProjectName: candidate.projectName,
		})
	}

	return &types.RemoteSessionIssuerDuplicatePreflight{Matches: matches}
}

// emptyIssuerDuplicatePreflight is the answer for a URL that is not a usable
// issuer identifier.
//
// A partially typed URL is the normal state of a form field, so this is a 200
// with nothing found rather than a 400. Returning an error would mean a stream
// of client-fault 4xx responses — each one marking its OpenTelemetry span as
// errored — for the entirely ordinary act of typing. It also matches how
// ListGlobalIssuerConvergenceCandidates treats a stored URL it cannot parse:
// narrow the match rather than fail the request.
func emptyIssuerDuplicatePreflight() *types.RemoteSessionIssuerDuplicatePreflight {
	return &types.RemoteSessionIssuerDuplicatePreflight{
		Matches: []*types.RemoteSessionIssuerDuplicateMatch{},
	}
}
