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
	// OrganizationID owns the admission, and is the whole of its tenancy.
	//
	// Deliberately no endpoint here. This answers "is this machine one of
	// ours", which does not change depending on which MCP server is asking;
	// what a recognised machine may reach is an authorization question that
	// RBAC answers per toolset. Keying an endpoint in as well would ask the
	// same question twice at two granularities, and leave this key disagreeing
	// with the workload principal it produces, which is organization-scoped on
	// exactly (issuer, subject).
	OrganizationID string
	// WorkloadIssuerID is the external issuer that vouches for it, named by
	// row rather than by URL so a discovery refresh or an in-place URL edit
	// cannot silently repoint an existing admission.
	WorkloadIssuerID uuid.UUID
	// ExternalSubject is the sub claim that issuer must assert. Named to stay
	// distinct from urn.SessionSubject, the Gram-side identity derived from it.
	ExternalSubject string
}

// workloadIdentityLookup reports whether an endpoint admits one workload
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
func newStaticWorkloadIdentityLookup(admitted ...workloadIdentity) workloadIdentityLookup {
	set := make(map[workloadIdentity]struct{}, len(admitted))
	for _, identity := range admitted {
		set[identity] = struct{}{}
	}

	return func(_ context.Context, identity workloadIdentity) (bool, error) {
		_, ok := set[identity]
		return ok, nil
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
		OrganizationID:   endpoint.OrganizationID,
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
