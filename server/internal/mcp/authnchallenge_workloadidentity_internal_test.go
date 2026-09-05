package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// workloadIdentityFixture is one admitted workload and the endpoint and issuer
// it was admitted against, so a test can vary exactly one part of the key.
type workloadIdentityFixture struct {
	endpoint *ResolvedMcpEndpoint
	issuer   *remotesessions_repo.RemoteSessionIssuer
	subject  string
}

func newWorkloadIdentityFixture() workloadIdentityFixture {
	return workloadIdentityFixture{
		endpoint: &ResolvedMcpEndpoint{
			OrganizationID:      uuid.NewString(),
			ProjectID:           uuid.New(),
			UserSessionIssuerID: uuid.New(),
		},
		issuer:  &remotesessions_repo.RemoteSessionIssuer{ID: uuid.New(), Slug: "gh-actions"},
		subject: "repo:acme/payments-api:ref:refs/heads/main",
	}
}

// identity is the admission this fixture stands for.
func (f workloadIdentityFixture) identity() workloadIdentity {
	return workloadIdentity{
		OrganizationID:        f.endpoint.OrganizationID,
		UserSessionIssuerID:   f.endpoint.UserSessionIssuerID,
		RemoteSessionIssuerID: f.issuer.ID,
		ExternalSubject:       f.subject,
	}
}

// admit runs the admission this fixture describes against lookup.
func (f workloadIdentityFixture) admit(t *testing.T, lookup workloadIdentityLookup) error {
	t.Helper()
	return admitWorkloadIdentity(t.Context(), lookup, f.endpoint, f.issuer, f.subject)
}

// A static policy naming exactly one subject admits that subject.
func TestAdmitWorkloadIdentity_AdmittedSubjectPasses(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	lookup := newStaticWorkloadIdentityLookup(fixture.identity())

	require.NoError(t, fixture.admit(t, lookup))
}

// The security boundary. A CI provider's issuer mints genuine assertions for
// every job on its platform, so an assertion that verifies against a trusted
// issuer must still be rejected when its subject names a workload nobody
// admitted.
func TestAdmitWorkloadIdentity_VerifiedButUnadmittedSubjectIsRejected(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	// Somebody else's job on the same trusted issuer.
	lookup := newStaticWorkloadIdentityLookup(workloadIdentity{
		OrganizationID:        fixture.endpoint.OrganizationID,
		UserSessionIssuerID:   fixture.endpoint.UserSessionIssuerID,
		RemoteSessionIssuerID: fixture.issuer.ID,
		ExternalSubject:       "repo:someone-else/their-api:ref:refs/heads/main",
	})

	require.ErrorIs(t, fixture.admit(t, lookup), errWorkloadNotAdmitted)
}

// An empty policy has to admit nothing rather than fall through to an
// allow-all.
func TestAdmitWorkloadIdentity_EmptyPolicyAdmitsNothing(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()

	require.ErrorIs(t, fixture.admit(t, newStaticWorkloadIdentityLookup()), errWorkloadNotAdmitted)
}

// An unwired policy must read as "no admissions", never as "no check to run":
// a lookup nobody supplied is the absence of permission, not the absence of a
// rule to apply.
func TestAdmitWorkloadIdentity_UnconfiguredLookupAdmitsNothing(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()

	require.ErrorIs(t, fixture.admit(t, nil), errWorkloadNotAdmitted)
}

// Every part of the key is load-bearing: sub is unique within an issuer and
// never across, one Gram issuer's admission must not answer for another's, and
// remote_session_issuers tenancy is application-enforced because its
// global-tier rows are shared. Varying one field at a time proves no part of
// the triple is being ignored.
func TestAdmitWorkloadIdentity_EveryPartOfTheKeyMustMatch(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	admitted := fixture.identity()

	for name, mutate := range map[string]func(workloadIdentity) workloadIdentity{
		"a different organization": func(i workloadIdentity) workloadIdentity {
			i.OrganizationID = uuid.NewString()
			return i
		},
		"a different endpoint issuer": func(i workloadIdentity) workloadIdentity {
			i.UserSessionIssuerID = uuid.New()
			return i
		},
		"a different external issuer": func(i workloadIdentity) workloadIdentity {
			i.RemoteSessionIssuerID = uuid.New()
			return i
		},
		"a different subject": func(i workloadIdentity) workloadIdentity {
			i.ExternalSubject = "repo:acme/payments-api:ref:refs/heads/other"
			return i
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// The policy admits the mutated identity; the request presents the
			// original. Only an exact match on all four may pass.
			lookup := newStaticWorkloadIdentityLookup(mutate(admitted))

			require.ErrorIs(t, fixture.admit(t, lookup), errWorkloadNotAdmitted,
				"%s must not be admitted by the original's entry", name)
		})
	}
}

// The same sub from two different issuers is two different workloads. Keying
// on the subject alone would let one issuer's admission answer for the other,
// which is the collision the identity is keyed on the pair to avoid.
func TestAdmitWorkloadIdentity_OneSubjectFromTwoIssuersDoesNotShareAnAdmission(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	lookup := newStaticWorkloadIdentityLookup(fixture.identity())

	// A second trusted issuer — one the organization runs itself, so it
	// controls every claim in it — asserting a byte-identical subject.
	staging := &remotesessions_repo.RemoteSessionIssuer{ID: uuid.New(), Slug: "staging-idp"}

	err := admitWorkloadIdentity(t.Context(), lookup, fixture.endpoint, staging, fixture.subject)

	require.ErrorIs(t, err, errWorkloadNotAdmitted, "an identical sub from another issuer is another workload")
}

// A subject the assertion never carried names no workload, and must not be
// able to match a row holding an empty string.
func TestAdmitWorkloadIdentity_EmptySubjectIsNeverAdmitted(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	empty := fixture.identity()
	empty.ExternalSubject = ""

	// Even with the empty subject explicitly in the policy.
	lookup := newStaticWorkloadIdentityLookup(empty)

	err := admitWorkloadIdentity(t.Context(), lookup, fixture.endpoint, fixture.issuer, "")

	require.ErrorIs(t, err, errWorkloadNotAdmitted)
}

// A store that fails to answer has not decided anything. Reporting it as
// non-admission would turn an outage into a rejection and deny workloads that
// are in fact admitted.
func TestAdmitWorkloadIdentity_LookupFailureIsNotARejection(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	outage := errors.New("connection refused")
	lookup := func(context.Context, workloadIdentity) (bool, error) { return false, outage }

	err := fixture.admit(t, lookup)

	require.ErrorIs(t, err, outage)
	require.NotErrorIs(t, err, errWorkloadNotAdmitted, "an outage is not an admission decision")
}

// A missing endpoint or issuer leaves the key unbuildable. It must fail closed
// rather than reach the lookup with a zero-valued tenancy, which could match a
// zero-valued entry.
func TestAdmitWorkloadIdentity_MissingTenancyOrIssuerAdmitsNothing(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()

	consulted := false
	lookup := func(context.Context, workloadIdentity) (bool, error) {
		consulted = true
		return true, nil
	}

	require.ErrorIs(t, admitWorkloadIdentity(t.Context(), lookup, nil, fixture.issuer, fixture.subject), errWorkloadNotAdmitted)
	require.ErrorIs(t, admitWorkloadIdentity(t.Context(), lookup, fixture.endpoint, nil, fixture.subject), errWorkloadNotAdmitted)
	require.False(t, consulted, "an unbuildable key must never reach the lookup")
}
