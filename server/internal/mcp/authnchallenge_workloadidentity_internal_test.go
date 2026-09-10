package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// workloadIdentityFixture is one workload plus the endpoint and issuer it was
// admitted against, so a test can vary exactly one part of the key.
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

// organizationTier is this fixture's admission, held above every project.
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

// The security boundary: a trusted issuer signs every job on its platform, so
// an unadmitted subject must still be rejected.
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

// Varying one field at a time proves no part of the key is ignored.
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

// The same sub from two issuers is two workloads.
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

// An empty subject must not match a row holding one.
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

// A store that fails to answer has decided nothing; an outage is not a
// rejection.
func TestAdmitWorkloadIdentity_LookupFailureIsNotARejection(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	outage := errors.New("connection refused")
	lookup := func(context.Context, workloadIdentity) (bool, error) { return false, outage }

	err := fixture.admit(t, lookup)

	require.ErrorIs(t, err, outage)
	require.NotErrorIs(t, err, errWorkloadNotAdmitted, "an outage is not an admission decision")
}

// An unbuildable key fails closed rather than reaching the lookup with a
// zero-valued tenancy.
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

// The organization tier is visible to every project beneath it.
func TestAdmitWorkloadIdentity_OrganizationTierAdmitsAnyProjectInIt(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	lookup := newStaticWorkloadIdentityLookup(fixture.organizationTier())

	// A second server in the same organization, in a different project.
	elsewhere := &ResolvedMcpEndpoint{OrganizationID: fixture.endpoint.OrganizationID, ProjectID: uuid.New()}

	require.NoError(t, admitWorkloadIdentity(t.Context(), lookup, elsewhere, fixture.issuerID, fixture.subject))
}

// A project admits its own workload without an organization administrator.
func TestAdmitWorkloadIdentity_ProjectTierAdmitsItsOwnProject(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	lookup := newStaticWorkloadIdentityLookup(fixture.projectTier(fixture.endpoint.ProjectID))

	require.NoError(t, fixture.admit(t, lookup))
}

// The isolation the tier exists for, and the one a lookup keyed on the
// organization alone silently loses.
func TestAdmitWorkloadIdentity_ASiblingProjectsAdmissionDoesNotAdmit(t *testing.T) {
	t.Parallel()

	fixture := newWorkloadIdentityFixture()
	sibling := uuid.New()
	lookup := newStaticWorkloadIdentityLookup(fixture.projectTier(sibling))

	require.ErrorIs(t, fixture.admit(t, lookup), errWorkloadNotAdmitted)
}

// The two nulls differ: an unset row answers everyone, an unset query is a
// caller with no project that only the organization tier may answer.
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

// Pins the guard, not a reachable state: no table row carries a zero project
// id, but two different meanings must not compare equal at a security boundary
// just because their zero values do.
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
