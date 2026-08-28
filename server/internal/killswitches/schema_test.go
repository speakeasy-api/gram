//nolint:glint // Constraint tests require invalid raw writes that production SQLc methods cannot express.
package killswitches

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/stretchr/testify/require"
)

func TestKillswitchSchemaConstraintsAndCascade(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "killswitch_schema")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	insertOrganization(t, conn, orgID)

	prescriptionID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescriptions (
			id, organization_id, definition_key, principal_kind, principal_key, resource_kind, current_version
		) VALUES ($1, $2, 'test_capability', 'user', 'user_1', 'test_resource', 1)
	`, prescriptionID, orgID)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_versions (
			organization_id, prescription_id, version, state, resource_scope, starts_at, expires_at, activated_at, internal_note, external_note
		) VALUES ($1, $2, 1, 'active', 'selected', clock_timestamp(), clock_timestamp() + interval '1 hour', clock_timestamp(), 'internal', 'external')
	`, orgID, prescriptionID)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_version_resources (organization_id, prescription_id, version, resource_key)
		VALUES ($1, $2, 1, 'resource_1')
	`, orgID, prescriptionID)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_expiry_events (organization_id, prescription_id, version)
		VALUES ($1, $2, 1)
	`, orgID, prescriptionID)
	require.NoError(t, err)

	operationID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_operations (organization_id, operation_id, actor_user_id, operation, request_hash, expires_at)
		VALUES ($1, $2, 'user_1', 'activate', 'request_hash', clock_timestamp() + interval '30 days')
	`, orgID, operationID)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `DELETE FROM organization_metadata WHERE id = $1`, orgID)
	require.NoError(t, err)

	var remainingRows int
	err = conn.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM killswitch_prescriptions)
		  + (SELECT count(*) FROM killswitch_prescription_versions)
		  + (SELECT count(*) FROM killswitch_prescription_version_resources)
		  + (SELECT count(*) FROM killswitch_expiry_events)
		  + (SELECT count(*) FROM killswitch_operations)
	`).Scan(&remainingRows)
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
	for _, tc := range prescriptionCases {
		_, err := conn.Exec(ctx, `
			INSERT INTO killswitch_prescriptions (organization_id, definition_key, principal_kind, principal_key, resource_kind, current_version)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, orgID, tc.definitionKey, tc.principalKind, tc.principalKey, tc.resourceKind, tc.currentVersion)
		requireConstraint(t, err, tc.constraint)
	}

	prescriptionID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescriptions (id, organization_id, definition_key, principal_kind, principal_key, resource_kind, current_version)
		VALUES ($1, $2, 'test_capability', 'user', 'user_1', 'test_resource', 1)
	`, prescriptionID, orgID)
	require.NoError(t, err)

	startsAt := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	versionCases := []struct {
		version       int64
		state         string
		resourceScope string
		expiresAt     any
		internalNote  string
		externalNote  string
		constraint    string
	}{
		{0, "active", "selected", nil, "internal", "external", "killswitch_prescription_versions_version_check"},
		{1, "", "selected", nil, "internal", "external", "killswitch_prescription_versions_state_check"},
		{1, "active", "some", nil, "internal", "external", "killswitch_prescription_versions_resource_scope_check"},
		{1, "active", "selected", startsAt, "internal", "external", "killswitch_prescription_versions_interval_check"},
		{1, "active", "selected", nil, "", "external", "killswitch_prescription_versions_internal_note_check"},
		{1, "active", "selected", nil, strings.Repeat("i", 4001), "external", "killswitch_prescription_versions_internal_note_check"},
		{1, "active", "selected", nil, "internal", "", "killswitch_prescription_versions_external_note_check"},
		{1, "active", "selected", nil, "internal", strings.Repeat("e", 501), "killswitch_prescription_versions_external_note_check"},
	}
	for _, tc := range versionCases {
		_, err := conn.Exec(ctx, `
			INSERT INTO killswitch_prescription_versions (
				organization_id, prescription_id, version, state, resource_scope, starts_at, expires_at, internal_note, external_note
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, orgID, prescriptionID, tc.version, tc.state, tc.resourceScope, startsAt, tc.expiresAt, tc.internalNote, tc.externalNote)
		requireConstraint(t, err, tc.constraint)
	}

	// PostgreSQL text cannot represent NUL. Application normalization rejects it first, and this
	// assertion documents the persistence boundary in case unnormalized input reaches the driver.
	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_versions (
			organization_id, prescription_id, version, state, resource_scope, starts_at, internal_note, external_note
		) VALUES ($1, $2, 1, 'active', 'selected', $3, $4, 'external')
	`, orgID, prescriptionID, startsAt, "internal\x00note")
	require.Error(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_versions (
			organization_id, prescription_id, version, state, resource_scope, starts_at, internal_note, external_note
		) VALUES ($1, $2, 1, 'active', 'selected', $3, 'internal', 'external')
	`, orgID, prescriptionID, startsAt)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_version_resources (organization_id, prescription_id, version, resource_key)
		VALUES ($1, $2, 1, '')
	`, orgID, prescriptionID)
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
	for _, tc := range operationCases {
		_, err := conn.Exec(ctx, `
			INSERT INTO killswitch_operations (organization_id, operation_id, actor_user_id, operation, request_hash, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, orgID, uuid.New(), tc.actorUserID, tc.operation, tc.requestHash, startsAt.Add(30*24*time.Hour))
		requireConstraint(t, err, tc.constraint)
	}

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `ALTER TABLE killswitch_operations DROP CONSTRAINT killswitch_operations_completed_response_check`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO killswitch_operations (organization_id, operation_id, actor_user_id, operation, request_hash, status, expires_at)
		VALUES ($1, $2, 'user_1', 'activate', 'request_hash', 'unknown', $3)
	`, orgID, uuid.New(), startsAt.Add(30*24*time.Hour))
	requireConstraint(t, err, "killswitch_operations_status_check")
	require.NoError(t, tx.Rollback(ctx))

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_operations (organization_id, operation_id, actor_user_id, operation, request_hash, status, response, expires_at)
		VALUES ($1, $2, 'user_1', 'activate', 'request_hash', 'completed', NULL, $3)
	`, orgID, uuid.New(), startsAt.Add(30*24*time.Hour))
	requireConstraint(t, err, "killswitch_operations_completed_response_check")

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_operations (organization_id, operation_id, actor_user_id, operation, request_hash, status, response, expires_at)
		VALUES ($1, $2, 'user_1', 'activate', 'request_hash', 'pending', jsonb_build_object('ok', true), $3)
	`, orgID, uuid.New(), startsAt.Add(30*24*time.Hour))
	requireConstraint(t, err, "killswitch_operations_completed_response_check")
}

func TestKillswitchSchemaPinsTenancyAndIdempotency(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "killswitch_tenancy")
	require.NoError(t, err)

	orgA := "org_" + uuid.NewString()
	orgB := "org_" + uuid.NewString()
	insertOrganization(t, conn, orgA)
	insertOrganization(t, conn, orgB)

	prescriptionID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescriptions (id, organization_id, definition_key, principal_kind, principal_key, resource_kind, current_version)
		VALUES ($1, $2, 'test_capability', 'user', 'user_1', 'test_resource', 1)
	`, prescriptionID, orgA)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_versions (
			organization_id, prescription_id, version, state, resource_scope, starts_at, internal_note, external_note
		) VALUES ($1, $2, 1, 'active', 'selected', clock_timestamp(), 'internal', 'external')
	`, orgB, prescriptionID)
	requireConstraint(t, err, "killswitch_prescription_versions_prescription_fkey")

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_versions (
			organization_id, prescription_id, version, state, resource_scope, starts_at, internal_note, external_note
		) VALUES ($1, $2, 1, 'active', 'selected', clock_timestamp(), 'internal', 'external')
	`, orgA, prescriptionID)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_versions (
			organization_id, prescription_id, version, state, resource_scope, starts_at, internal_note, external_note
		) VALUES ($1, $2, 1, 'active', 'selected', clock_timestamp(), 'internal', 'external')
	`, orgA, prescriptionID)
	requireConstraint(t, err, "killswitch_prescription_versions_pkey")

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_version_resources (organization_id, prescription_id, version, resource_key)
		VALUES ($1, $2, 1, 'resource_1')
	`, orgB, prescriptionID)
	requireConstraint(t, err, "killswitch_prescription_version_resources_version_fkey")

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_version_resources (organization_id, prescription_id, version, resource_key)
		VALUES ($1, $2, 1, 'resource_1')
	`, orgA, prescriptionID)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_prescription_version_resources (organization_id, prescription_id, version, resource_key)
		VALUES ($1, $2, 1, 'resource_1')
	`, orgA, prescriptionID)
	requireConstraint(t, err, "killswitch_prescription_version_resources_pkey")

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_expiry_events (organization_id, prescription_id, version)
		VALUES ($1, $2, 1)
	`, orgB, prescriptionID)
	requireConstraint(t, err, "killswitch_expiry_events_prescription_version_fkey")

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_expiry_events (organization_id, prescription_id, version)
		VALUES ($1, $2, 1)
	`, orgA, prescriptionID)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_expiry_events (organization_id, prescription_id, version)
		VALUES ($1, $2, 1)
	`, orgA, prescriptionID)
	requireConstraint(t, err, "killswitch_expiry_events_pkey")

	operationID := uuid.New()
	for _, orgID := range []string{orgA, orgB} {
		_, err = conn.Exec(ctx, `
			INSERT INTO killswitch_operations (organization_id, operation_id, actor_user_id, operation, request_hash, expires_at)
			VALUES ($1, $2, 'user_1', 'activate', 'request_hash', clock_timestamp() + interval '30 days')
		`, orgID, operationID)
		require.NoError(t, err)
	}

	_, err = conn.Exec(ctx, `
		INSERT INTO killswitch_operations (organization_id, operation_id, actor_user_id, operation, request_hash, expires_at)
		VALUES ($1, $2, 'user_1', 'activate', 'request_hash', clock_timestamp() + interval '30 days')
	`, orgA, operationID)
	requireConstraint(t, err, "killswitch_operations_pkey")
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
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT ARRAY(
			SELECT pg_get_indexdef(indexrelid, position, true)
			FROM generate_series(1, indnkeyatts) AS position
			ORDER BY position
		), pg_get_expr(indpred, indrelid), access_method.amname
		FROM pg_index
		JOIN pg_class index_class ON index_class.oid = indexrelid
		JOIN pg_am access_method ON access_method.oid = index_class.relam
		WHERE indexrelid = 'killswitch_prescription_versions_expiry_due_idx'::regclass
	`).Scan(&indexColumns, &indexPredicate, &indexAccessMethod))
	require.Equal(t, []string{"expires_at", "prescription_id", "version"}, indexColumns)
	require.Equal(t, "((state = 'active'::text) AND (expires_at IS NOT NULL) AND (expiry_event_recorded_at IS NULL) AND ((superseded_at IS NULL) OR (expires_at < superseded_at)))", indexPredicate)
	require.Equal(t, "btree", indexAccessMethod)
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
