package remotesessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// ErrIssuerURLInvalid marks a candidate issuer URL that is not an issuer
// identifier at all — wrong scheme, no host, or carrying userinfo, a query, or
// a fragment. Distinguished from a lookup that simply found nothing so callers
// can answer a malformed request differently from an unknown one.
var ErrIssuerURLInvalid = errors.New("invalid issuer url")

// IssuerLookup names one upstream authorization server and the tenancy it is
// being resolved for.
type IssuerLookup struct {
	// IssuerURL is the candidate identifier, as supplied. It is canonicalized
	// here rather than by the caller.
	IssuerURL string

	// ProjectID and OrganizationID are the tiers the lookup may draw on. Both
	// inherited tiers are always in scope: an organization-level or platform
	// issuer describing this upstream is one the project may use.
	ProjectID      uuid.UUID
	OrganizationID string
}

// ResolveIssuerByURL returns the single issuer row a project resolves an
// upstream authorization server URL onto, or false when no tier-visible row
// describes it.
//
// This is the shared implementation behind both the management API's
// resolve-by-issuer path and workload assertion admission. It is one function
// because the tenancy predicate and the precedence ladder are the security
// boundary in both: a second copy that drifted would either hide an issuer a
// project may legitimately use or, worse, admit one from another tenant.
//
// Purely a read. Nothing here fetches, probes, or discovers — a caller holding
// an unresolvable URL learns only that Gram has no row for it, which is what
// keeps a request-supplied issuer from becoming an outbound request.
func ResolveIssuerByURL(ctx context.Context, db repo.DBTX, lookup IssuerLookup) (repo.RemoteSessionIssuer, bool, error) {
	canonical, err := parseCanonicalIssuerURL(lookup.IssuerURL)
	if err != nil {
		return repo.RemoteSessionIssuer{}, false, fmt.Errorf("%w: %w", ErrIssuerURLInvalid, err)
	}

	candidates, err := repo.New(db).ListRemoteSessionIssuersByIssuerURL(ctx, repo.ListRemoteSessionIssuersByIssuerURLParams{
		Issuers:               canonical.matchCandidates(),
		ProjectID:             uuid.NullUUID{UUID: lookup.ProjectID, Valid: true},
		IncludeOrganizational: true,
		OrganizationID:        conv.ToPGText(lookup.OrganizationID),
		IncludeGlobal:         true,
	})
	if err != nil {
		return repo.RemoteSessionIssuer{}, false, fmt.Errorf("list remote session issuers by issuer url: %w", err)
	}

	// An issuer URL can match several rows: duplicates across tiers are
	// legitimate by design, so precedence decides which one applies.
	match, found := resolveIssuerByPrecedence(candidates)
	return match, found, nil
}
