// Package workloadidentity resolves the external issuers and workload subjects
// an organization has said it trusts for the RFC 7523 §2.1 workload assertion
// grant.
package workloadidentity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

// ErrIssuerURLInvalid reports a value that is not an issuer identifier at all.
//
// Distinct from ErrIssuerNotFound on purpose: a caller answers a malformed
// request differently from an unknown one, and collapsing the two would make a
// client's own bug indistinguishable from an unconfigured issuer.
var ErrIssuerURLInvalid = errors.New("invalid issuer url")

// ErrIssuerNotFound reports a well-formed issuer identifier this tenant has no
// row for. It carries no evidence about whether some other tenant does.
var ErrIssuerNotFound = errors.New("workload issuer not found")

// ResolveIssuerParams addresses one lookup. Tenancy is not optional and not a
// flag: there is no arm that reads outside the caller's organization.
type ResolveIssuerParams struct {
	// OrganizationID scopes every row considered. Empty resolves nothing.
	OrganizationID string
	// ProjectID selects the project tier when set. Unset is an
	// organization-scoped caller, which sees only organization-tier rows.
	ProjectID uuid.NullUUID
	// IssuerURL is the assertion's iss claim, exactly as presented.
	IssuerURL string
}

// ResolveIssuerByURL returns the workload issuer row this tenant trusts for an
// issuer identifier, or ErrIssuerNotFound.
//
// Nothing here fetches, probes, or discovers. A caller holding an unresolvable
// URL learns only that we have no row for it, which is what stops a
// request-supplied issuer from turning into an outbound request.
//
// There is no platform tier: trust in a workload issuer is a row in the
// tenant's own project or organization, so no shared catalog exists that a
// customer could silently land on.
func ResolveIssuerByURL(ctx context.Context, db repo.DBTX, params ResolveIssuerParams) (repo.WorkloadIssuer, error) {
	canonical, err := issuerurl.Parse(params.IssuerURL)
	if err != nil {
		return repo.WorkloadIssuer{}, fmt.Errorf("%w: %w", ErrIssuerURLInvalid, err)
	}

	// Checked after the parse and before the store, so a caller with no
	// organization matches nothing rather than everything. The query would
	// already return no rows, since organization_id is NOT NULL and no row
	// carries an empty one, but a tenancy hole should not depend on a data
	// property that a future seed or fixture could break.
	if params.OrganizationID == "" {
		return repo.WorkloadIssuer{}, ErrIssuerNotFound
	}

	candidates, err := repo.New(db).ListWorkloadIssuersByIssuerURL(ctx, repo.ListWorkloadIssuersByIssuerURLParams{
		Issuers:        canonical.MatchCandidates(),
		OrganizationID: params.OrganizationID,
		ProjectID:      params.ProjectID,
	})
	if err != nil {
		return repo.WorkloadIssuer{}, fmt.Errorf("list workload issuers by issuer url: %w", err)
	}

	match, found := resolveByPrecedence(candidates)
	if !found {
		return repo.WorkloadIssuer{}, ErrIssuerNotFound
	}

	return match, nil
}

// resolveByPrecedence picks the row a tenant should use when an issuer URL
// matches more than one.
//
// Duplicates across tiers are legitimate: an organization registers an issuer
// once, and a project later registers its own row for the same platform to
// carry different admissions. The project's own row wins, and oldest wins
// within a tier.
//
// The caller's query supplies the oldest-first order, so this only has to
// prefer the narrower tier and otherwise keep the first row it sees.
func resolveByPrecedence(candidates []repo.WorkloadIssuer) (repo.WorkloadIssuer, bool) {
	var best repo.WorkloadIssuer
	found := false

	for _, candidate := range candidates {
		if !found || scopeOf(candidate) < scopeOf(best) {
			best = candidate
			found = true
		}
	}

	return best, found
}

// scopeOf ranks a row's tenancy tier, narrower first. Two tiers only: a
// workload issuer is either the project's own or the organization's, and there
// is no platform rung below.
func scopeOf(issuer repo.WorkloadIssuer) int {
	if issuer.ProjectID.Valid {
		return 0
	}

	return 1
}
