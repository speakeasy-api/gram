// Admission for the workload assertion grant: deciding whether a verified
// assertion's subject names a workload this organization recognises. Runs after
// workloadIssuerKeySource in authnchallenge_workloadauth.go has resolved the
// keys, and after verification against them succeeds.

package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// errWorkloadNotAdmitted reports a genuine assertion whose subject names no
// workload identity this organization has admitted.
//
// Distinct from errWorkloadIssuerUntrusted, which rejects the issuer before
// anything is verified. Reaching this one means the assertion is real and its
// issuer is trusted; the remaining question is whether the machine is ours.
var errWorkloadNotAdmitted = errors.New("workload identity is not admitted by this organization")

// workloadIdentity names one admitted workload. Every field is part of the
// key, and the struct is comparable so exact match is a property of the type
// rather than a convention each caller has to keep.
//
// There is deliberately no pattern, prefix, or wildcard field: these platforms
// put declared, bounded resources in sub — a service account, a repository and
// environment pairing — and wildcarding a CI subject is the misconfiguration
// that hands production credentials to anyone able to push a branch. Widening
// this is an additive match_kind column if a customer ever needs it.
type workloadIdentity struct {
	// OrganizationID scopes every admission that could answer. No arm reads
	// outside it.
	OrganizationID string
	// ProjectID selects the project tier when set. Unset is an
	// organization-scoped caller, which sees only organization-tier
	// admissions. Same shape and same meaning as
	// workloadidentity.ResolveIssuerParams.ProjectID, so the two reads on this
	// path scope alike.
	//
	// Deliberately no endpoint here. Which MCP server is asking must not
	// change the answer — what a recognised machine may then reach is an
	// authorization question RBAC answers per toolset. A project is coarser
	// than that and is a tier of the admission itself: workload_identity_
	// admissions rows sit either in one project or at the organization above
	// them, so a team can recognise its own workload without an organization
	// administrator admitting it for them.
	ProjectID uuid.NullUUID
	// WorkloadIssuerID is the external issuer that vouches for it, named by
	// row rather than by URL so a discovery refresh or an in-place URL edit
	// cannot silently repoint an existing admission.
	WorkloadIssuerID uuid.UUID
	// ExternalSubject is the sub claim that issuer must assert. Named to stay
	// distinct from urn.SessionSubject, the Gram-side identity derived from it.
	ExternalSubject string
}

// workloadAdmission is one stored admission: what a policy holds, as opposed
// to workloadIdentity, which is what a caller asks about.
//
// They are separate types because the project field does not mean the same
// thing on each. On a query it is the project asking and is always set; on a
// row it is the tier, and an unset one is the organization tier that every
// project in that organization sees. Collapsing them into one struct would
// make an org-tier row indistinguishable from a query with no project, which
// is the confusion that lets a project-tier admission leak organization-wide.
type workloadAdmission struct {
	OrganizationID string
	// ProjectID unset is the organization tier. Set is that project alone.
	ProjectID        uuid.NullUUID
	WorkloadIssuerID uuid.UUID
	ExternalSubject  string
}

// admits reports whether this row answers a query, matching the tiers the way
// the table's indexes do: every other component by exact equality, and the
// project by tier, so an organization-tier row answers any project in its
// organization and a project-tier row answers only its own.
//
// Both sides are nullable and the nulls mean different things, which is the
// whole reason the two types are separate. An unset row is the organization
// tier and answers everyone; an unset query is an organization-scoped caller,
// which a project-tier row must not answer. SQL gets this for free — the
// project arm of `project_id = @project_id OR project_id IS NULL` is simply
// not true when the parameter is NULL — and this mirrors it rather than
// inventing a second rule.
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

// workloadIdentityLookup reports whether an organization admits one workload
// identity. Injected so admission can be exercised against a static policy
// with no database behind it, and so a store can replace that policy without
// touching a caller.
//
// False and an error are different answers. False is a decision. An error is
// the absence of one, and callers must never read it as a rejection, or a
// store outage would start denying workloads that are in fact admitted.
type workloadIdentityLookup func(ctx context.Context, identity workloadIdentity) (bool, error)

// newStaticWorkloadIdentityLookup admits exactly the identities given.
//
// With none, it admits nothing. There is no allow-all and no configuration
// that produces one: the grant this serves is reachable without credentials,
// so a policy that failed open would admit every machine its issuers ever mint
// a token for.
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

// admitWorkloadIdentity reports nil when the endpoint's organization admits
// externalSubject from the given workload issuer, and errWorkloadNotAdmitted
// when it does not.
//
// This is the security boundary of the feature, and a separate question from
// the one the signature answered. A CI provider's issuer mints valid, signed
// assertions for every job on its platform, that provider's other customers
// included — so trusting GitHub Actions without naming the subject would admit
// anybody's GitHub Actions.
//
// Every case that could otherwise be read as permission fails closed.
func admitWorkloadIdentity(
	ctx context.Context,
	lookup workloadIdentityLookup,
	endpoint *ResolvedMcpEndpoint,
	workloadIssuerID uuid.UUID,
	externalSubject string,
) error {
	switch {
	case lookup == nil:
		// An unwired policy is the production default until a store is
		// configured, and "no policy" reads as "no admissions" rather than a
		// panic or a skip.
		return errWorkloadNotAdmitted
	case endpoint == nil || workloadIssuerID == uuid.Nil:
		// No key can be built, so no row could answer for it.
		return errWorkloadNotAdmitted
	case externalSubject == "":
		// Rejected rather than looked up, so an empty string can never match a
		// row that happens to hold one.
		return errWorkloadNotAdmitted
	}

	admitted, err := lookup(ctx, workloadIdentity{
		OrganizationID: endpoint.OrganizationID,
		// A zero project is an endpoint that names none, which asks as an
		// organization-scoped caller rather than as project uuid.Nil — no row
		// carries that id, but a sentinel comparing equal by accident is not a
		// property worth relying on at a security boundary.
		ProjectID:        uuid.NullUUID{UUID: endpoint.ProjectID, Valid: endpoint.ProjectID != uuid.Nil},
		WorkloadIssuerID: workloadIssuerID,
		ExternalSubject:  externalSubject,
	})
	if err != nil {
		// Never a rejection: the store failed to answer, which is not evidence
		// that this workload is unadmitted. A caller mapping non-admission
		// onto a 403 must not turn an outage into one.
		return fmt.Errorf("resolve admitted workload identity: %w", err)
	}
	if !admitted {
		return errWorkloadNotAdmitted
	}

	return nil
}
