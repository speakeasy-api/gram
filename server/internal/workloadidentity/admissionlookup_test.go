package workloadidentity_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

const testSubject = "repo:acme/payments-api:ref:refs/heads/main"

// seedAdmission writes one workload_identity_admissions row directly. Raw SQL
// for the same reason seedIssuer uses it: writes are the management API's job,
// and this package is read-only by design.
func seedAdmission(t *testing.T, conn *pgxpool.Pool, organizationID string, projectID uuid.NullUUID, issuerID uuid.UUID, subject string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := conn.QueryRow( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		t.Context(), `
		INSERT INTO workload_identity_admissions
		  (organization_id, project_id, workload_issuer_id, subject)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, organizationID, projectID, issuerID, subject).Scan(&id)
	require.NoError(t, err)

	return id
}

func withdraw(t *testing.T, conn *pgxpool.Pool, id uuid.UUID) {
	t.Helper()

	_, err := conn.Exec( //nolint:glint // notestingrawsql: see seedAdmission
		t.Context(), `UPDATE workload_identity_admissions SET deleted_at = clock_timestamp() WHERE id = $1`, id)
	require.NoError(t, err)
}

// admissionFixture is one tenant with one issuer.
type admissionFixture struct {
	tenant   tenant
	issuerID uuid.UUID
}

func newAdmissionFixture(t *testing.T, conn *pgxpool.Pool) admissionFixture {
	t.Helper()

	tenant := newTenant(t, conn)

	return admissionFixture{
		tenant:   tenant,
		issuerID: seedIssuer(t, conn, tenant.organizationID, organizationTier(), "gh-actions", testIssuerURL, epoch),
	}
}

func (f admissionFixture) params() workloadidentity.AdmissionParams {
	return workloadidentity.AdmissionParams{
		OrganizationID:   f.tenant.organizationID,
		ProjectID:        projectTier(f.tenant.projectID),
		WorkloadIssuerID: f.issuerID,
		Subject:          testSubject,
	}
}

func TestIsAdmitted_AnAdmittedSubjectResolves(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAdmissionFixture(t, conn)
	seedAdmission(t, conn, fixture.tenant.organizationID, organizationTier(), fixture.issuerID, testSubject)

	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, fixture.params())

	require.NoError(t, err)
	require.True(t, admitted)
}

// A verified assertion proves the platform minted it, not that it is ours.
func TestIsAdmitted_AnUnadmittedSubjectDoesNot(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAdmissionFixture(t, conn)
	// Somebody else's job on the same trusted issuer.
	seedAdmission(t, conn, fixture.tenant.organizationID, organizationTier(), fixture.issuerID, "repo:someone-else/their-api:ref:refs/heads/main")

	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, fixture.params())

	require.NoError(t, err)
	require.False(t, admitted)
}

// Changing any single component must not resolve.
func TestIsAdmitted_EveryComponentOfTheKeyMustMatch(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAdmissionFixture(t, conn)
	seedAdmission(t, conn, fixture.tenant.organizationID, organizationTier(), fixture.issuerID, testSubject)

	other := newTenant(t, conn)
	otherIssuer := seedIssuer(t, conn, fixture.tenant.organizationID, organizationTier(), "staging", "https://staging.example.test", epoch)

	for name, mutate := range map[string]func(workloadidentity.AdmissionParams) workloadidentity.AdmissionParams{
		"another organization": func(p workloadidentity.AdmissionParams) workloadidentity.AdmissionParams {
			p.OrganizationID = other.organizationID
			return p
		},
		"another issuer": func(p workloadidentity.AdmissionParams) workloadidentity.AdmissionParams {
			p.WorkloadIssuerID = otherIssuer
			return p
		},
		"another subject": func(p workloadidentity.AdmissionParams) workloadidentity.AdmissionParams {
			p.Subject = testSubject + ":other"
			return p
		},
		"an empty organization": func(p workloadidentity.AdmissionParams) workloadidentity.AdmissionParams {
			p.OrganizationID = ""
			return p
		},
		"an empty subject": func(p workloadidentity.AdmissionParams) workloadidentity.AdmissionParams {
			p.Subject = ""
			return p
		},
		"no issuer": func(p workloadidentity.AdmissionParams) workloadidentity.AdmissionParams {
			p.WorkloadIssuerID = uuid.Nil
			return p
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, mutate(fixture.params()))

			require.NoError(t, err)
			require.False(t, admitted, "%s must not resolve against the admitted row", name)
		})
	}
}

// The organization tier is visible to every project beneath it.
func TestIsAdmitted_TheOrganizationTierAnswersEveryProject(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAdmissionFixture(t, conn)
	seedAdmission(t, conn, fixture.tenant.organizationID, organizationTier(), fixture.issuerID, testSubject)

	elsewhere := fixture.params()
	elsewhere.ProjectID = projectTier(newProject(t, conn, fixture.tenant.organizationID))

	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, elsewhere)

	require.NoError(t, err)
	require.True(t, admitted)
}

// A project admits its own workload without an organization administrator.
func TestIsAdmitted_TheProjectTierAnswersItsOwnProject(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAdmissionFixture(t, conn)
	seedAdmission(t, conn, fixture.tenant.organizationID, projectTier(fixture.tenant.projectID), fixture.issuerID, testSubject)

	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, fixture.params())

	require.NoError(t, err)
	require.True(t, admitted)
}

// The isolation the tier exists for, and the case a lookup keyed on the
// organization alone silently loses.
func TestIsAdmitted_ASiblingProjectsAdmissionDoesNot(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAdmissionFixture(t, conn)
	sibling := newProject(t, conn, fixture.tenant.organizationID)
	seedAdmission(t, conn, fixture.tenant.organizationID, projectTier(sibling), fixture.issuerID, testSubject)

	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, fixture.params())

	require.NoError(t, err)
	require.False(t, admitted)
}

// A project's private admission must not answer a caller naming no project.
func TestIsAdmitted_AnOrganizationScopedCallerSeesOnlyTheOrganizationTier(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAdmissionFixture(t, conn)
	projectRow := seedAdmission(t, conn, fixture.tenant.organizationID, projectTier(fixture.tenant.projectID), fixture.issuerID, testSubject)

	organizationScoped := fixture.params()
	organizationScoped.ProjectID = organizationTier()

	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, organizationScoped)
	require.NoError(t, err)
	require.False(t, admitted, "a project's admission must not answer a caller that named no project")

	// The same caller, once the organization itself admits the subject.
	withdraw(t, conn, projectRow)
	seedAdmission(t, conn, fixture.tenant.organizationID, organizationTier(), fixture.issuerID, testSubject)

	admitted, err = workloadidentity.IsAdmitted(t.Context(), conn, organizationScoped)
	require.NoError(t, err)
	require.True(t, admitted)
}

// Withdrawal is a soft delete; ignoring it would keep admitting a workload an
// administrator believes they have removed.
func TestIsAdmitted_AWithdrawnAdmissionDoesNotResolve(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAdmissionFixture(t, conn)
	id := seedAdmission(t, conn, fixture.tenant.organizationID, organizationTier(), fixture.issuerID, testSubject)

	withdraw(t, conn, id)

	admitted, err := workloadidentity.IsAdmitted(t.Context(), conn, fixture.params())

	require.NoError(t, err)
	require.False(t, admitted)
}
