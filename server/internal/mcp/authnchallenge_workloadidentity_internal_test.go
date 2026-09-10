package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// workloadIdentityFixture is one admitted workload and the endpoint and issuer
// it was admitted against, so a test can vary exactly one part of the key.
type workloadIdentityFixture struct {
	endpoint *ResolvedMcpEndpoint
	issuerID uuid.UUID
	subject  string
}

func newWorkloadIdentityFixture() workloadIdentityFixture {
	return workloadIdentityFixture{
		endpoint: &ResolvedMcpEndpoint{
			OrganizationID: uuid.NewString(),
			ProjectID:      uuid.New(),
		},
		issuerID: uuid.New(),
		subject:  "repo:acme/payments-api:ref:refs/heads/main",
	}
}

// identity is the query this fixture's endpoint makes.
func (f workloadIdentityFixture) identity() workloadIdentity {
	return workloadIdentity{
		OrganizationID:   f.endpoint.OrganizationID,
		ProjectID:        uuid.NullUUID{UUID: f.endpoint.ProjectID, Valid: true},
		WorkloadIssuerID: f.issuerID,
		ExternalSubject:  f.subject,
	}
}

// organizationTier is the stored admission this fixture stands for, held above
// every project in the organization.
func (f workloadIdentityFixture) organizationTier() workloadAdmission {
	return workloadAdmission{
		OrganizationID:   f.endpoint.OrganizationID,
		ProjectID:        uuid.NullUUID{},
		WorkloadIssuerID: f.issuerID,
		ExternalSubject:  f.subject,
	}
}

// projectTier is the same admission held by one project alone.
func (f workloadIdentityFixture) projectTier(projectID uuid.UUID) workloadAdmission {
	row := f.organizationTier()
	row.ProjectID = uuid.NullUUID{UUID: projectID, Valid: true}

	return row
}

// admit runs the admission this fixture describes against lookup.
func (f workloadIdentityFixture) admit(t *testing.T, lookup workloadIdentityLookup) error {
	t.Helper()
	return admitWorkloadIdentity(t.Context(), lookup, f.endpoint, f.issuerID, f.subject)
}

// A static policy naming exactly one subject admits that subject.
func TestAdmitWorkloadIdentity_AdmittedSubjectPasses(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	lookup := newStaticWorkloadIdentityLookup(fixture.organizationTier())

	require.NoError(t, fixture.admit(t, lookup))
}

// The security boundary. A CI provider's issuer mints genuine assertions for
// every job on its platform, so verifying against a trusted issuer is not
// enough: an unadmitted subject must still be rejected.
func TestAdmitWorkloadIdentity_VerifiedButUnadmittedSubjectIsRejected(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	// Somebody else's job on the same trusted issuer.
	lookup := newStaticWorkloadIdentityLookup(workloadAdmission{
		OrganizationID:   fixture.endpoint.OrganizationID,
		WorkloadIssuerID: fixture.issuerID,
		ExternalSubject:  "repo:someone-else/their-api:ref:refs/heads/main",
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

// An unwired policy reads as "no admissions", never as "no check to run".
func TestAdmitWorkloadIdentity_UnconfiguredLookupAdmitsNothing(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()

	require.ErrorIs(t, fixture.admit(t, nil), errWorkloadNotAdmitted)
}

// Every part of the key is load-bearing: sub is unique within an issuer and
// never across, and one Gram issuer's admission must not answer for another's.
// Varying one field at a time proves no part is being ignored.
func TestAdmitWorkloadIdentity_EveryPartOfTheKeyMustMatch(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	admitted := fixture.organizationTier()

	for name, mutate := range map[string]func(workloadAdmission) workloadAdmission{
		"a different organization": func(a workloadAdmission) workloadAdmission {
			a.OrganizationID = uuid.NewString()
			return a
		},
		"a different external issuer": func(a workloadAdmission) workloadAdmission {
			a.WorkloadIssuerID = uuid.New()
			return a
		},
		"a different subject": func(a workloadAdmission) workloadAdmission {
			a.ExternalSubject = "repo:acme/payments-api:ref:refs/heads/other"
			return a
		},
		"another project's admission": func(a workloadAdmission) workloadAdmission {
			a.ProjectID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
			return a
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
	lookup := newStaticWorkloadIdentityLookup(fixture.organizationTier())

	// A second trusted issuer — one the organization runs itself, so it
	// controls every claim in it — asserting a byte-identical subject.
	staging := uuid.New()

	err := admitWorkloadIdentity(t.Context(), lookup, fixture.endpoint, staging, fixture.subject)

	require.ErrorIs(t, err, errWorkloadNotAdmitted, "an identical sub from another issuer is another workload")
}

// A subject the assertion never carried names no workload, and must not be
// able to match a row holding an empty string.
func TestAdmitWorkloadIdentity_EmptySubjectIsNeverAdmitted(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	empty := fixture.organizationTier()
	empty.ExternalSubject = ""

	// Even with the empty subject explicitly in the policy.
	lookup := newStaticWorkloadIdentityLookup(empty)

	err := admitWorkloadIdentity(t.Context(), lookup, fixture.endpoint, fixture.issuerID, "")

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

// A missing endpoint or issuer leaves the key unbuildable. It fails closed
// rather than reaching the lookup with a zero-valued tenancy, which could
// match a zero-valued entry.
func TestAdmitWorkloadIdentity_MissingTenancyOrIssuerAdmitsNothing(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()

	consulted := false
	lookup := func(context.Context, workloadIdentity) (bool, error) {
		consulted = true
		return true, nil
	}

	require.ErrorIs(t, admitWorkloadIdentity(t.Context(), lookup, nil, fixture.issuerID, fixture.subject), errWorkloadNotAdmitted)
	require.ErrorIs(t, admitWorkloadIdentity(t.Context(), lookup, fixture.endpoint, uuid.Nil, fixture.subject), errWorkloadNotAdmitted)
	require.False(t, consulted, "an unbuildable key must never reach the lookup")
}

// The organization tier is visible to every project beneath it, which is what
// makes an administrator's admission worth making once.
func TestAdmitWorkloadIdentity_OrganizationTierAdmitsAnyProjectInIt(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	lookup := newStaticWorkloadIdentityLookup(fixture.organizationTier())

	// A second server in the same organization, in a different project.
	elsewhere := &ResolvedMcpEndpoint{OrganizationID: fixture.endpoint.OrganizationID, ProjectID: uuid.New()}

	require.NoError(t, admitWorkloadIdentity(t.Context(), lookup, elsewhere, fixture.issuerID, fixture.subject))
}

// A project-tier admission answers for its own project, which is the point of
// the tier: a team recognises its own workload without an organization
// administrator admitting it for them.
func TestAdmitWorkloadIdentity_ProjectTierAdmitsItsOwnProject(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	lookup := newStaticWorkloadIdentityLookup(fixture.projectTier(fixture.endpoint.ProjectID))

	require.NoError(t, fixture.admit(t, lookup))
}

// TestAdmitWorkloadIdentity_ASiblingProjectsAdmissionDoesNotAdmit is the
// isolation the project tier exists for, and the one a lookup keyed on the
// organization alone would silently lose: one team's decision to trust a
// workload must not admit it across the whole organization.
func TestAdmitWorkloadIdentity_ASiblingProjectsAdmissionDoesNotAdmit(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	sibling := uuid.New()
	lookup := newStaticWorkloadIdentityLookup(fixture.projectTier(sibling))

	require.ErrorIs(t, fixture.admit(t, lookup), errWorkloadNotAdmitted)
}

// An organization-scoped caller names no project, and a project-tier admission
// must not answer it. The nulls on the two sides are not the same null: an
// unset row is the organization tier and answers everyone, an unset query is a
// caller with no project and may only be answered by that tier.
func TestAdmitWorkloadIdentity_AnOrganizationScopedCallerSeesOnlyTheOrganizationTier(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	// An endpoint naming no project asks as the organization.
	organizationScoped := &ResolvedMcpEndpoint{OrganizationID: fixture.endpoint.OrganizationID}

	projectTier := newStaticWorkloadIdentityLookup(fixture.projectTier(fixture.endpoint.ProjectID))
	require.ErrorIs(t,
		admitWorkloadIdentity(t.Context(), projectTier, organizationScoped, fixture.issuerID, fixture.subject),
		errWorkloadNotAdmitted,
		"a project's admission must not answer a caller that named no project")

	organizationTier := newStaticWorkloadIdentityLookup(fixture.organizationTier())
	require.NoError(t,
		admitWorkloadIdentity(t.Context(), organizationTier, organizationScoped, fixture.issuerID, fixture.subject))
}

// A row claiming the project tier while carrying the zero uuid must not answer
// an organization-scoped caller, whose unset project also reads as zero. No
// such row can come from the table — a project id is generated, never zero —
// so this pins the guard rather than a reachable state: at a security
// boundary, two different meanings must not compare equal just because their
// zero values do.
func TestAdmitWorkloadIdentity_AZeroProjectTierRowAnswersNobody(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	malformed := fixture.organizationTier()
	malformed.ProjectID = uuid.NullUUID{UUID: uuid.Nil, Valid: true}
	lookup := newStaticWorkloadIdentityLookup(malformed)

	organizationScoped := &ResolvedMcpEndpoint{OrganizationID: fixture.endpoint.OrganizationID}

	require.ErrorIs(t,
		admitWorkloadIdentity(t.Context(), lookup, organizationScoped, fixture.issuerID, fixture.subject),
		errWorkloadNotAdmitted)
}
