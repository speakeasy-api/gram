// Admission for the workload assertion grant: whether a verified assertion's
// subject names a workload this tenant recognises. Runs after verification.

package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// errWorkloadNotAdmitted reports a genuine assertion from a trusted issuer
// whose subject nobody admitted. Distinct from errWorkloadIssuerUntrusted,
// which rejects the issuer before anything is verified.
var errWorkloadNotAdmitted = errors.New("workload identity is not admitted by this organization")

// workloadIdentity is one admission query. Every field is part of the key.
//
// No pattern, prefix or wildcard field: wildcarding a CI subject is the
// misconfiguration that hands production credentials to anyone able to push a
// branch. Widening it later is an additive match_kind column.
type workloadIdentity struct {
	// OrganizationID scopes every admission that could answer.
	OrganizationID string
	// ProjectID is the project asking. Unset is an organization-scoped caller,
	// which sees only organization-tier admissions. Matches
	// workloadidentity.ResolveIssuerParams.ProjectID so both reads on this
	// path scope alike.
	//
	// No endpoint: which MCP server is asking must not change the answer. What
	// a recognised machine may then reach is RBAC's question, per toolset.
	ProjectID uuid.NullUUID
	// WorkloadIssuerID names the issuer by row rather than by URL, so a
	// discovery refresh cannot silently repoint an existing admission.
	WorkloadIssuerID uuid.UUID
	// ExternalSubject is the sub claim the issuer asserted. Named to stay
	// distinct from urn.SessionSubject, the Gram identity derived from it.
	ExternalSubject string
}

// workloadAdmission is one stored admission, kept separate from
// workloadIdentity because the project field differs: on a query it is the
// project asking, on a row it is the tier. One struct for both would make an
// organization-tier row indistinguishable from a query naming no project,
// which is what lets a project's admission leak organization-wide.
type workloadAdmission struct {
	OrganizationID string
	// ProjectID unset is the organization tier. Set is that project alone.
	ProjectID        uuid.NullUUID
	WorkloadIssuerID uuid.UUID
	ExternalSubject  string
}

// admits matches every component by equality and the project by tier: an
// unset row is the organization tier and answers everyone, an unset query is
// an organization-scoped caller a project-tier row must not answer.
//
// Mirrors what `project_id = @project_id OR project_id IS NULL` does in SQL,
// where the project arm is not true for a NULL parameter.
func (a workloadAdmission) admits(identity workloadIdentity) bool {
	if a.OrganizationID != identity.OrganizationID ||
		a.WorkloadIssuerID != identity.WorkloadIssuerID ||
		a.ExternalSubject != identity.ExternalSubject {
		return false
	}
	if !a.ProjectID.Valid {
		return true
	}

	return identity.ProjectID.Valid && a.ProjectID.UUID == identity.ProjectID.UUID
}

// workloadIdentityLookup reports whether a tenant admits one workload.
// Injected so admission is testable without a database.
//
// False and an error are different answers: false is a decision, an error is
// the absence of one. Reading an error as a rejection would turn a store
// outage into denials for workloads that are in fact admitted.
type workloadIdentityLookup func(ctx context.Context, identity workloadIdentity) (bool, error)

// newStaticWorkloadIdentityLookup admits exactly the rows given, and with none
// admits nothing. There is no allow-all: this grant is reachable without
// credentials, so failing open would admit every machine its issuers serve.
func newStaticWorkloadIdentityLookup(admitted ...workloadAdmission) workloadIdentityLookup {
	rows := make([]workloadAdmission, len(admitted))
	copy(rows, admitted)

	return func(_ context.Context, identity workloadIdentity) (bool, error) {
		for _, row := range rows {
			if row.admits(identity) {
				return true, nil
			}
		}

		return false, nil
	}
}

// admitWorkloadIdentity reports nil when the endpoint's tenant admits
// externalSubject from this issuer, errWorkloadNotAdmitted when it does not.
//
// The security boundary, and a different question from the one the signature
// answered: a CI provider signs valid assertions for every job on its
// platform, so trusting the issuer without naming the subject would admit
// anybody's. Every ambiguous case fails closed.
func admitWorkloadIdentity(
	ctx context.Context,
	lookup workloadIdentityLookup,
	endpoint *ResolvedMcpEndpoint,
	workloadIssuerID uuid.UUID,
	externalSubject string,
) error {
	switch {
	// An unwired policy reads as "no admissions", an unbuildable key as "no
	// row could answer", and an empty subject is refused rather than looked up
	// so it can never match a row holding one.
	case lookup == nil, endpoint == nil, workloadIssuerID == uuid.Nil, externalSubject == "":
		return errWorkloadNotAdmitted
	}

	admitted, err := lookup(ctx, workloadIdentity{
		OrganizationID: endpoint.OrganizationID,
		// A zero project asks as an organization-scoped caller rather than as
		// project uuid.Nil: a sentinel comparing equal by accident is not a
		// property to rely on at a security boundary.
		ProjectID:        uuid.NullUUID{UUID: endpoint.ProjectID, Valid: endpoint.ProjectID != uuid.Nil},
		WorkloadIssuerID: workloadIssuerID,
		ExternalSubject:  externalSubject,
	})
	if err != nil {
		// Never a rejection: a caller mapping non-admission onto a 403 must
		// not turn a store outage into one.
		return fmt.Errorf("resolve admitted workload identity: %w", err)
	}
	if !admitted {
		return errWorkloadNotAdmitted
	}

	return nil
}
