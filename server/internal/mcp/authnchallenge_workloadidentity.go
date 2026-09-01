// Admission for the workload assertion grant: deciding whether a verified
// assertion's subject names a workload this endpoint admits. Runs after
// workloadIssuerKeySource in authnchallenge_workloadauth.go has resolved the
// keys an assertion verifies against, and after that verification succeeds.

package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// errWorkloadNotAdmitted reports a genuine assertion whose subject names no
// workload identity this endpoint admits.
//
// Distinct from errWorkloadIssuerUntrusted, which rejects the issuer before
// anything is verified. Reaching this error means the assertion is real and
// its issuer is trusted — the remaining question is whether the machine it
// vouches for is one of ours.
var errWorkloadNotAdmitted = errors.New("workload identity is not admitted by this endpoint")

// workloadIdentity names one admitted workload: the exact triple the admission
// index is keyed on, plus the organization the row belongs to.
//
// Every field is part of the key, and the struct is comparable so that
// "exact match" is a property of the type rather than a convention a caller
// has to keep. There is deliberately no pattern, prefix, or wildcard field:
// these platforms put declared, bounded resources in sub — a service account,
// a registered workload, a repository and environment pairing — and
// wildcarding a CI subject is the misconfiguration that hands production
// credentials to anyone able to push a branch. Widening this is an additive
// match_kind column when a customer needs it, not a shape to leave open now.
//
// OrganizationID is part of the key even though the database's unique index is
// the three columns after it. remote_session_issuers holds global-tier rows
// with organization_id IS NULL that any organization may reference, so that
// table's tenancy is application-enforced by design (AIM-143). Carrying the
// organization here is that enforcement at this layer: a lookup cannot answer
// for a tenancy it was not asked about.
type workloadIdentity struct {
	// OrganizationID owns the admission.
	OrganizationID string
	// UserSessionIssuerID is the Gram endpoint issuer this identity may
	// obtain a session against.
	UserSessionIssuerID uuid.UUID
	// RemoteSessionIssuerID is the external issuer that vouches for it.
	RemoteSessionIssuerID uuid.UUID
	// ExternalSubject is the sub claim that issuer must assert. Named to stay
	// distinct from urn.SessionSubject, which is the Gram-side identity
	// derived from it rather than the value the platform put in the token.
	ExternalSubject string
}

// workloadIdentityLookup reports whether an endpoint admits one workload
// identity.
//
// Injected so that admission can be exercised against a static policy with no
// database behind it, and so a store can replace that policy without touching
// a caller. A database-backed implementation is the same triple queried
// against user_session_issuer_workload_identities, restricted to rows that are
// enabled and not soft-deleted.
//
// Reporting false and reporting an error are different answers. False is a
// decision — this endpoint does not admit this workload. An error is the
// absence of one, and callers must never read it as a rejection, or a store
// outage would start denying workloads that are in fact admitted.
type workloadIdentityLookup func(ctx context.Context, identity workloadIdentity) (bool, error)

// newStaticWorkloadIdentityLookup admits exactly the identities given and
// nothing else.
//
// Calling it with no identities admits nothing, which is the safe default and
// the whole contract: there is no allow-all here and no configuration that
// produces one. The grant this serves is reachable without credentials, so a
// policy that failed open on a misconfiguration would admit every machine its
// issuers ever mint a token for.
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

// admitWorkloadIdentity reports nil when endpoint admits externalSubject from
// issuer, and errWorkloadNotAdmitted when it does not.
//
// This is the security boundary of the feature, and it is a separate question
// from the one the signature answered. A CI provider's issuer mints valid,
// correctly signed assertions for every job on its platform — every one of
// that provider's own customers included. Trusting the issuer establishes that
// an assertion is genuine; only naming the subject establishes that the
// machine is ours. Without this step, trusting GitHub Actions would admit
// anybody's GitHub Actions.
//
// Fails closed at every step that could otherwise be read as permission: an
// unconfigured lookup, a subject the assertion never carried, and an issuer
// that resolved to nothing are all non-admission rather than a reason to skip
// the check.
func admitWorkloadIdentity(
	ctx context.Context,
	lookup workloadIdentityLookup,
	endpoint *ResolvedMcpEndpoint,
	issuer *remotesessions_repo.RemoteSessionIssuer,
	externalSubject string,
) error {
	switch {
	case lookup == nil:
		// Nothing is configured, so nothing is admitted. Deliberately not a
		// programming error: an unwired policy is the production default until
		// a store is configured, and the safe reading of "no policy" is "no
		// admissions" rather than a panic that takes the endpoint down or a
		// skip that lets everything through.
		return errWorkloadNotAdmitted
	case endpoint == nil || issuer == nil:
		// The tenancy or the issuer the admission would be keyed on is
		// missing, so no key can be built and no row could answer for it.
		return errWorkloadNotAdmitted
	case externalSubject == "":
		// An assertion carrying no subject names no workload. Rejected here
		// rather than looked up, so an empty string can never match a row that
		// happens to hold one.
		return errWorkloadNotAdmitted
	}

	admitted, err := lookup(ctx, workloadIdentity{
		OrganizationID:        endpoint.OrganizationID,
		UserSessionIssuerID:   endpoint.UserSessionIssuerID,
		RemoteSessionIssuerID: issuer.ID,
		ExternalSubject:       externalSubject,
	})
	if err != nil {
		// Never reported as a rejection: the store failed to answer, which is
		// not evidence that this workload is unadmitted. A caller mapping
		// non-admission onto a 403 must not turn an outage into one.
		return fmt.Errorf("resolve admitted workload identity: %w", err)
	}
	if !admitted {
		return errWorkloadNotAdmitted
	}

	return nil
}
