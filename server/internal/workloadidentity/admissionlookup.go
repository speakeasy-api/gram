package workloadidentity

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

// AdmissionParams addresses one admission check. Every field is part of the
// key; none of them is optional in the sense that omitting it would widen the
// search.
type AdmissionParams struct {
	// OrganizationID scopes every row considered. Empty admits nothing.
	OrganizationID string
	// ProjectID selects the project tier when set. Unset is an
	// organization-scoped caller, which sees only organization-tier
	// admissions — a project's own decision never answers it. Same shape and
	// meaning as ResolveIssuerParams.ProjectID.
	ProjectID uuid.NullUUID
	// WorkloadIssuerID is the workload_issuers row that vouches for the
	// subject, never the issuer URL: re-registering an issuer is deliberately
	// a new identity, and a discovery refresh must not silently repoint an
	// existing admission.
	WorkloadIssuerID uuid.UUID
	// Subject is the sub claim the issuer asserted, exactly as it arrived.
	Subject string
}

// IsAdmitted reports whether a tenant recognises one workload as its own.
//
// This is the security boundary of the workload grant, and a different
// question from the one the signature answered. A CI provider's issuer mints
// genuine, correctly signed assertions for every job on its platform — every
// one of that provider's other customers included — so a verified assertion
// says only that the platform minted it. Presence here is what makes the
// machine ours.
//
// False and an error are different answers, and a caller must never collapse
// them. False is a decision. An error is the absence of one: a store outage
// reported as non-admission would deny workloads that are in fact admitted,
// and reported as admission would be far worse.
func IsAdmitted(ctx context.Context, db repo.DBTX, params AdmissionParams) (bool, error) {
	// Checked before the store rather than left to the query, for the same
	// reason ResolveIssuerByURL checks its organization: a tenancy or key hole
	// should not depend on a data property a future seed or fixture could
	// break. An empty subject must never match a row that happens to hold one.
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
