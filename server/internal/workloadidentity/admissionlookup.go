package workloadidentity

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

// AdmissionParams addresses one admission check. Every field is part of the
// key; omitting one never widens the search.
type AdmissionParams struct {
	// OrganizationID scopes every row considered. Empty admits nothing.
	OrganizationID string
	// ProjectID selects the project tier when set. Unset is an
	// organization-scoped caller, which a project's own admission never
	// answers. Same shape as ResolveIssuerParams.ProjectID.
	ProjectID uuid.NullUUID
	// WorkloadIssuerID is the row that vouches for the subject, never the
	// issuer URL: a discovery refresh must not repoint an existing admission.
	WorkloadIssuerID uuid.UUID
	// Subject is the sub claim the issuer asserted, exactly as it arrived.
	Subject string
}

// IsAdmitted reports whether a tenant recognises one workload as its own.
//
// The security boundary of the grant. A CI provider signs genuine assertions
// for every job on its platform, its other customers included, so a verified
// assertion says only that the platform minted it. Presence here is what makes
// the machine ours.
//
// An admission matches the whole subject, or the stem of a `subject*` rule when
// the row says wildcard and its issuer permits wildcard matching. Wildcard exists
// for platforms that mint an identity per resource, where the subject cannot be
// known before the first assertion arrives. See MatchKind for why the `*` is
// mandatory and where it may appear; for where wildcard matching is sound at all,
// see the allow_wildcard_admission comment in the schema, which is why that gate
// sits on the issuer rather than on the rule.
//
// False and an error must never be collapsed: false is a decision, an error is
// the absence of one.
func IsAdmitted(ctx context.Context, db repo.DBTX, params AdmissionParams) (bool, error) {
	// Checked before the store, like ResolveIssuerByURL's organization: a
	// tenancy hole should not depend on a data property a seed could break.
	switch {
	case params.OrganizationID == "", params.WorkloadIssuerID == uuid.Nil, params.Subject == "":
		return false, nil
	}

	admitted, err := repo.New(db).WorkloadIdentityIsAdmitted(ctx, repo.WorkloadIdentityIsAdmittedParams{
		OrganizationID:   params.OrganizationID,
		ProjectID:        params.ProjectID,
		WorkloadIssuerID: params.WorkloadIssuerID,
		Subject:          params.Subject,
	})
	if err != nil {
		return false, fmt.Errorf("check workload identity admission: %w", err)
	}

	return admitted, nil
}
