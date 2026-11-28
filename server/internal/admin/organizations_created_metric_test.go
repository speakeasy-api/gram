package admin

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics/orgmetricstest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestCreateOrganization_RecordsPlatformAdminCreation(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZMETRICADMINCREATE"
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, newFakeWorkOS(workosOrgID))
	created, reader := orgmetricstest.New(t)
	svc.orgsCreated = created

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "metric.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, orgid.FromWorkOSID(workosOrgID), res.ID)
	require.EqualValues(t, 1, countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID))

	reader.RequireOnly(t, orgmetrics.CreationSourcePlatformAdmin, 1)
}

// The WorkOS sync inserts the row first, so the admin upsert only updates it
// and the creation belongs to the sync.
func TestCreateOrganization_RowInsertedBySyncRecordsNoCreation(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZMETRICSYNCFIRST"
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, newFakeWorkOS(workosOrgID))
	created, reader := orgmetricstest.New(t)
	svc.orgsCreated = created

	runOrganizationWebhook(t, ctx, conn, workosOrgID, organizationEvent("event_01HZMETRICSYNC", "organization.created", workosOrgID, "Sync First Co", ""))
	_, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, orgid.FromWorkOSID(workosOrgID))
	require.NoError(t, err, "the sync inserts the row before the admin create")

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "syncfirst.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, orgid.FromWorkOSID(workosOrgID), res.ID)
	require.EqualValues(t, 1, countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID))

	require.Empty(t, reader.Counts(t))
}

// The upsert inserts the row inside the transaction, but a later write fails
// and rolls it back, so nothing is recorded.
func TestCreateOrganization_RolledBackCreateRecordsNoCreation(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZMETRICROLLBACK"
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, newFakeWorkOS(workosOrgID))
	created, reader := orgmetricstest.New(t)
	svc.orgsCreated = created

	err := repo.New(conn).RejectOrganizationEntitlementsFixture(ctx)
	require.NoError(t, err)

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "metricrollback.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.Error(t, err)
	require.Nil(t, res)
	require.Contains(t, oops.Detail(err), "seed organization default entitlements")
	requireNoOrganizationRow(t, ctx, conn, workosOrgID)

	require.Empty(t, reader.Counts(t))
}

func TestCreateOrganization_WorkOSRejectionRecordsNoCreation(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZMETRICREJECTED"
	fake := newFakeWorkOS(workosOrgID)
	fake.createErr = errors.New("workos said no")
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)
	created, reader := orgmetricstest.New(t)
	svc.orgsCreated = created

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "metricrejected.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeGatewayError)
	require.Nil(t, res)
	requireNoOrganizationRow(t, ctx, conn, workosOrgID)

	require.Empty(t, reader.Counts(t))
}

// A create whose transaction commits is counted even when reading the
// organization back for the response fails afterwards.
func TestCreateOrganization_CommittedCreateRecordsCreationWhenResponseReadFails(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZMETRICREADFAIL"
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, newFakeWorkOS(workosOrgID))
	created, reader := orgmetricstest.New(t)
	svc.orgsCreated = created

	_, err := conn.Exec(ctx, "ALTER TABLE billing_metadata RENAME TO unavailable_billing_metadata") //nolint:glint // notestingrawsql: DDL fault injection in an isolated per-test database; SQLc cannot rename a table.
	require.NoError(t, err)

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "metricreadfail.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.Nil(t, res)
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.Contains(t, oops.Detail(err), "fetch organization after create")
	require.EqualValues(t, 1, countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID))

	reader.RequireOnly(t, orgmetrics.CreationSourcePlatformAdmin, 1)
}
