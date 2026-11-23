package killswitches

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	killswitchrepo "github.com/speakeasy-api/gram/server/internal/killswitches/repo"
	"github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestKillswitchSchemaConstraintsAndCascade(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "killswitch_schema")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	insertOrganization(t, conn, orgID)

	queries := killswitchrepo.New(conn)
	clock, err := queries.GetKillswitchDatabaseTime(ctx)
	require.NoError(t, err)
	prescriptionID, err := queries.CreateKillswitchPrescriptionHeader(ctx, killswitchrepo.CreateKillswitchPrescriptionHeaderParams{
		OrganizationID: orgID, DefinitionKey: "test_capability", PrincipalKind: "user", PrincipalKey: "user_1", ResourceKind: "test_resource",
	})
	require.NoError(t, err)

	inserted, err := queries.CreateKillswitchPrescriptionVersion(ctx, killswitchrepo.CreateKillswitchPrescriptionVersionParams{
		OrganizationID: orgID, PrescriptionID: prescriptionID, Version: 1, State: "active", ResourceScope: "selected",
		StartsAt: clock, ExpiresAt: conv.ToPGTimestamptz(clock.Time.Add(time.Hour)), ActivatedAt: clock,
		InternalNote: "internal", ExternalNote: "external",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)

	inserted, err = queries.CreateKillswitchPrescriptionVersionResources(ctx, killswitchrepo.CreateKillswitchPrescriptionVersionResourcesParams{
		OrganizationID: orgID, PrescriptionID: prescriptionID, Version: 1, ResourceKeys: []string{"resource_1"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)

	inserted, err = queries.RecordKillswitchExpiryEvent(ctx, killswitchrepo.RecordKillswitchExpiryEventParams{OrganizationID: orgID, PrescriptionID: prescriptionID, Version: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)

	_, err = queries.ClaimKillswitchOperation(ctx, killswitchrepo.ClaimKillswitchOperationParams{
		OrganizationID: orgID, OperationID: uuid.New(), ActorUserID: "user_1", Operation: "activate", RequestHash: "request_hash",
	})
	require.NoError(t, err)

	err = queries.DeleteKillswitchOrganizationFixture(ctx, orgID)
	require.NoError(t, err)

	remainingRows, err := queries.CountAllKillswitchRowsFixture(ctx)
	require.NoError(t, err)
	require.Zero(t, remainingRows)
}

func TestKillswitchSchemaRejectsInvalidRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "killswitch_constraints")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	insertOrganization(t, conn, orgID)

	prescriptionCases := []struct {
		definitionKey  string
		principalKind  string
		principalKey   string
		resourceKind   string
		currentVersion int64
		constraint     string
	}{
		{"", "user", "user_1", "test_resource", 1, "killswitch_prescriptions_definition_key_check"},
		{"test_capability", "", "user_1", "test_resource", 1, "killswitch_prescriptions_principal_kind_check"},
		{"test_capability", "user", "", "test_resource", 1, "killswitch_prescriptions_principal_key_check"},
		{"test_capability", "user", "user_1", "", 1, "killswitch_prescriptions_resource_kind_check"},
		{"test_capability", "user", "user_1", "test_resource", 0, "killswitch_prescriptions_current_version_check"},
	}
	queries := killswitchrepo.New(conn)
	for _, tc := range prescriptionCases {
		err := queries.InsertKillswitchPrescriptionRowFixture(ctx, killswitchrepo.InsertKillswitchPrescriptionRowFixtureParams{
			ID: uuid.New(), OrganizationID: orgID, DefinitionKey: tc.definitionKey, PrincipalKind: tc.principalKind,
			PrincipalKey: tc.principalKey, ResourceKind: tc.resourceKind, CurrentVersion: tc.currentVersion,
		})
		requireConstraint(t, err, tc.constraint)
	}

	prescriptionID := uuid.New()
	err = queries.InsertKillswitchPrescriptionRowFixture(ctx, testPrescriptionRow(prescriptionID, orgID))
	require.NoError(t, err)

	startsAt := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	versionCases := []struct {
		version       int64
		state         string
		resourceScope string
		expiresAt     pgtype.Timestamptz
		internalNote  string
		externalNote  string
		constraint    string
	}{
		{0, "active", "selected", pgtype.Timestamptz{}, "internal", "external", "killswitch_prescription_versions_version_check"},
		{1, "", "selected", pgtype.Timestamptz{}, "internal", "external", "killswitch_prescription_versions_state_check"},
		{1, "active", "some", pgtype.Timestamptz{}, "internal", "external", "killswitch_prescription_versions_resource_scope_check"},
		{1, "active", "selected", conv.ToPGTimestamptz(startsAt), "internal", "external", "killswitch_prescription_versions_interval_check"},
		{1, "active", "selected", pgtype.Timestamptz{}, "", "external", "killswitch_prescription_versions_internal_note_check"},
		{1, "active", "selected", pgtype.Timestamptz{}, strings.Repeat("i", 4001), "external", "killswitch_prescription_versions_internal_note_check"},
		{1, "active", "selected", pgtype.Timestamptz{}, "internal", "", "killswitch_prescription_versions_external_note_check"},
		{1, "active", "selected", pgtype.Timestamptz{}, "internal", strings.Repeat("e", 501), "killswitch_prescription_versions_external_note_check"},
	}
	for _, tc := range versionCases {
		err := queries.InsertKillswitchPrescriptionVersionRowFixture(ctx, killswitchrepo.InsertKillswitchPrescriptionVersionRowFixtureParams{
			OrganizationID: orgID, PrescriptionID: prescriptionID, Version: tc.version, State: tc.state, ResourceScope: tc.resourceScope,
			StartsAt: conv.ToPGTimestamptz(startsAt), ExpiresAt: tc.expiresAt, InternalNote: tc.internalNote, ExternalNote: tc.externalNote,
		})
		requireConstraint(t, err, tc.constraint)
	}

	// PostgreSQL text cannot represent NUL. Application normalization rejects it first, and this
	// assertion documents the persistence boundary in case unnormalized input reaches the driver.
	err = queries.InsertKillswitchPrescriptionVersionRowFixture(ctx, killswitchrepo.InsertKillswitchPrescriptionVersionRowFixtureParams{
		OrganizationID: orgID, PrescriptionID: prescriptionID, Version: 1, State: "active", ResourceScope: "selected",
		StartsAt: conv.ToPGTimestamptz(startsAt), ExpiresAt: pgtype.Timestamptz{}, InternalNote: "internal\x00note", ExternalNote: "external",
	})
	require.Error(t, err)

	err = queries.InsertKillswitchPrescriptionVersionRowFixture(ctx, killswitchrepo.InsertKillswitchPrescriptionVersionRowFixtureParams{
		OrganizationID: orgID, PrescriptionID: prescriptionID, Version: 1, State: "active", ResourceScope: "selected",
		StartsAt: conv.ToPGTimestamptz(startsAt), ExpiresAt: pgtype.Timestamptz{}, InternalNote: "internal", ExternalNote: "external",
	})
	require.NoError(t, err)

	err = queries.InsertKillswitchVersionResourceFixture(ctx, killswitchrepo.InsertKillswitchVersionResourceFixtureParams{OrganizationID: orgID, PrescriptionID: prescriptionID, ResourceKey: ""})
	requireConstraint(t, err, "killswitch_prescription_version_resources_resource_key_check")

	operationCases := []struct {
		actorUserID string
		operation   string
		requestHash string
		constraint  string
	}{
		{"", "activate", "request_hash", "killswitch_operations_actor_user_id_check"},
		{"user_1", "", "request_hash", "killswitch_operations_operation_check"},
		{"user_1", "activate", "", "killswitch_operations_request_hash_check"},
	}
	operationExpiresAt := conv.ToPGTimestamptz(startsAt.Add(30 * 24 * time.Hour))
	for _, tc := range operationCases {
		err := queries.InsertKillswitchOperationFixture(ctx, killswitchrepo.InsertKillswitchOperationFixtureParams{
			OrganizationID: orgID, OperationID: uuid.New(), ActorUserID: tc.actorUserID, Operation: tc.operation,
			RequestHash: tc.requestHash, Status: "pending", Response: nil, ExpiresAt: operationExpiresAt,
		})
		requireConstraint(t, err, tc.constraint)
	}

	//nolint:glint // notestingrawsql: transaction scopes the constraint drop below so it rolls back
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	//nolint:glint // notestingrawsql: drops a check constraint to isolate the status constraint; SQLc cannot express DDL
	_, err = tx.Exec(ctx, `ALTER TABLE killswitch_operations DROP CONSTRAINT killswitch_operations_completed_response_check`)
	require.NoError(t, err)
	err = killswitchrepo.New(tx).InsertKillswitchOperationFixture(ctx, testOperationRow(orgID, "unknown", nil, operationExpiresAt))
	requireConstraint(t, err, "killswitch_operations_status_check")
	require.NoError(t, tx.Rollback(ctx))

	err = queries.InsertKillswitchOperationFixture(ctx, testOperationRow(orgID, "completed", nil, operationExpiresAt))
	requireConstraint(t, err, "killswitch_operations_completed_response_check")

	err = queries.InsertKillswitchOperationFixture(ctx, testOperationRow(orgID, "pending", []byte(`{"ok": true}`), operationExpiresAt))
	requireConstraint(t, err, "killswitch_operations_completed_response_check")
}

func testPrescriptionRow(id uuid.UUID, orgID string) killswitchrepo.InsertKillswitchPrescriptionRowFixtureParams {
	return killswitchrepo.InsertKillswitchPrescriptionRowFixtureParams{
		ID: id, OrganizationID: orgID, DefinitionKey: "test_capability", PrincipalKind: "user",
		PrincipalKey: "user_1", ResourceKind: "test_resource", CurrentVersion: 1,
	}
}

func testOperationRow(orgID, status string, response []byte, expiresAt pgtype.Timestamptz) killswitchrepo.InsertKillswitchOperationFixtureParams {
	return killswitchrepo.InsertKillswitchOperationFixtureParams{
		OrganizationID: orgID, OperationID: uuid.New(), ActorUserID: "user_1", Operation: "activate",
		RequestHash: "request_hash", Status: status, Response: response, ExpiresAt: expiresAt,
	}
}

// thirtyDays is an interval of 30 calendar days, matching interval '30 days'.
var thirtyDays = pgtype.Interval{Microseconds: 0, Days: 30, Months: 0, Valid: true}

func TestKillswitchSchemaPinsTenancyAndIdempotency(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "killswitch_tenancy")
	require.NoError(t, err)

	orgA := "org_" + uuid.NewString()
	orgB := "org_" + uuid.NewString()
	insertOrganization(t, conn, orgA)
	insertOrganization(t, conn, orgB)

	queries := killswitchrepo.New(conn)
	prescriptionID := uuid.New()
	err = queries.InsertKillswitchPrescriptionRowFixture(ctx, testPrescriptionRow(prescriptionID, orgA))
	require.NoError(t, err)

	versionNow := func(orgID string) error {
		return queries.InsertActiveKillswitchPrescriptionVersionNowFixture(ctx, killswitchrepo.InsertActiveKillswitchPrescriptionVersionNowFixtureParams{OrganizationID: orgID, PrescriptionID: prescriptionID})
	}
	requireConstraint(t, versionNow(orgB), "killswitch_prescription_versions_prescription_fkey")
	require.NoError(t, versionNow(orgA))
	requireConstraint(t, versionNow(orgA), "killswitch_prescription_versions_pkey")

	versionResource := func(orgID string) error {
		return queries.InsertKillswitchVersionResourceFixture(ctx, killswitchrepo.InsertKillswitchVersionResourceFixtureParams{OrganizationID: orgID, PrescriptionID: prescriptionID, ResourceKey: "resource_1"})
	}
	requireConstraint(t, versionResource(orgB), "killswitch_prescription_version_resources_version_fkey")
	require.NoError(t, versionResource(orgA))
	requireConstraint(t, versionResource(orgA), "killswitch_prescription_version_resources_pkey")

	expiryEvent := func(orgID string) error {
		return queries.InsertKillswitchExpiryEventFixture(ctx, killswitchrepo.InsertKillswitchExpiryEventFixtureParams{OrganizationID: orgID, PrescriptionID: prescriptionID})
	}
	requireConstraint(t, expiryEvent(orgB), "killswitch_expiry_events_prescription_version_fkey")
	require.NoError(t, expiryEvent(orgA))
	requireConstraint(t, expiryEvent(orgA), "killswitch_expiry_events_pkey")

	operationID := uuid.New()
	operation := func(orgID string) error {
		return queries.InsertKillswitchOperationExpiringInFixture(ctx, killswitchrepo.InsertKillswitchOperationExpiringInFixtureParams{
			OrganizationID: orgID, OperationID: operationID, ActorUserID: "user_1", Operation: "activate", RequestHash: "request_hash", ExpiresIn: thirtyDays,
		})
	}
	for _, orgID := range []string{orgA, orgB} {
		require.NoError(t, operation(orgID))
	}
	requireConstraint(t, operation(orgA), "killswitch_operations_pkey")
}

func TestKillswitchExpiryDiscoveryIndexMatchesEligibilityAndOrder(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "killswitch_expiry_index")
	require.NoError(t, err)

	var (
		indexColumns      []string
		indexPredicate    string
		indexAccessMethod string
	)
	//nolint:glint // notestingrawsql: inspects index metadata in the system catalogs
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT ARRAY(
			SELECT pg_get_indexdef(index_metadata.indexrelid, position, true)
			FROM generate_series(1, index_metadata.indnkeyatts) AS position
			ORDER BY position
		), pg_get_expr(index_metadata.indpred, index_metadata.indrelid), access_method.amname
		FROM pg_index AS index_metadata
		JOIN pg_class AS index_relation ON index_relation.oid = index_metadata.indexrelid
		JOIN pg_am AS access_method ON access_method.oid = index_relation.relam
		WHERE index_metadata.indexrelid = 'killswitch_prescription_versions_expiry_due_idx'::regclass
	`).Scan(&indexColumns, &indexPredicate, &indexAccessMethod))
	require.Equal(t, []string{"expires_at", "prescription_id", "version"}, indexColumns)
	require.Equal(t, "btree", indexAccessMethod)
	require.Equal(t, "((state = 'active'::text) AND (expires_at IS NOT NULL) AND ((superseded_at IS NULL) OR (expires_at < superseded_at)))", indexPredicate)
}

func TestInsertKillswitchPrescriptionFixtureValidatesResourceScope(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "killswitch_fixture_scope")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	insertOrganization(t, conn, organizationID)
	fixtures := testrepo.New(conn)

	testCases := []struct {
		name          string
		resourceScope ResourceScope
		resourceKeys  []string
		wantErr       bool
	}{
		{name: "all without resources", resourceScope: ResourceScopeAll},
		{name: "selected with resources", resourceScope: ResourceScopeSelected, resourceKeys: []string{"resource_1"}},
		{name: "all with resources", resourceScope: ResourceScopeAll, resourceKeys: []string{"resource_1"}, wantErr: true},
		{name: "selected without resources", resourceScope: ResourceScopeSelected, wantErr: true},
		{name: "invalid scope", resourceScope: ResourceScope("invalid"), wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prescriptionID := uuid.New()
			err := fixtures.InsertKillswitchPrescriptionFixture(t.Context(), testrepo.InsertKillswitchPrescriptionFixtureParams{
				ResourceKeys:   testCase.resourceKeys,
				PrescriptionID: prescriptionID,
				OrganizationID: organizationID,
				DefinitionKey:  "test_capability",
				PrincipalKind:  "user",
				PrincipalKey:   "user_1",
				ResourceKind:   "test_resource",
				ResourceScope:  string(testCase.resourceScope),
				InternalNote:   "internal",
				ExternalNote:   "external",
			})
			if !testCase.wantErr {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			_, err = killswitchrepo.New(conn).GetKillswitchPrescriptionIdentity(t.Context(), killswitchrepo.GetKillswitchPrescriptionIdentityParams{
				OrganizationID: organizationID, PrescriptionID: prescriptionID,
			})
			require.ErrorIs(t, err, pgx.ErrNoRows, "a rejected fixture must roll back its prescription header")
		})
	}
}

func insertOrganization(t *testing.T, conn *pgxpool.Pool, organizationID string) {
	t.Helper()

	err := repo.New(conn).CreateOrganizationMetadata(t.Context(), repo.CreateOrganizationMetadataParams{
		ID:   organizationID,
		Name: "Test Organization",
		Slug: organizationID,
	})
	require.NoError(t, err)
}

func requireConstraint(t *testing.T, err error, constraint string) {
	t.Helper()

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, constraint, pgErr.ConstraintName)
}
