//nolint:glint,paralleltest // Integration fixtures intentionally create private rows with raw SQL in one isolated database.
package killswitches

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches/repo"
)

type capturingEvaluationDB struct {
	repo.DBTX
	query string
	args  []any
}

func (db *capturingEvaluationDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	db.query = query
	db.args = args
	return db.DBTX.QueryRow(ctx, query, args...)
}

type evaluationFixture struct {
	ID             uuid.UUID
	OrganizationID string
	DefinitionKey  string
	PrincipalKind  string
	PrincipalKey   string
	ResourceKind   string
	CurrentVersion int64
	Version        int64
	State          string
	Scope          string
	Immediate      bool
	StartsAt       time.Time
	ExpiresAt      *time.Time
	ActivatedAt    *time.Time
	ExternalNote   string
	Resources      []string
}

func TestEvaluateCurrentPrescriptionsIntegration(t *testing.T) {
	conn, organizationID := newLifecycleDatabase(t, "killswitch_evaluator")
	queries := repo.New(conn)
	var databaseNow time.Time
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&databaseNow))
	past := databaseNow.Add(-time.Hour)
	older := databaseNow.Add(-2 * time.Hour)
	future := databaseNow.Add(time.Hour)
	activeUntil := databaseNow.Add(time.Hour)
	activated := databaseNow.Add(-30 * time.Minute)
	newerActivation := databaseNow.Add(-10 * time.Minute)

	insert := func(fixture evaluationFixture) {
		fixture.OrganizationID = organizationID
		fixture.ResourceKind = "tool"
		if fixture.CurrentVersion == 0 {
			fixture.CurrentVersion = fixture.Version
		}
		insertEvaluationFixture(t, conn, fixture)
	}
	evaluate := func(principalKinds, principalKeys, definitions []string, resource string) (repo.EvaluateCurrentPrescriptionsRow, error) {
		compatibleDefinitions := make([]string, 0, len(definitions)*len(principalKinds))
		compatiblePrincipalKinds := make([]string, 0, len(definitions)*len(principalKinds))
		for _, definition := range definitions {
			for _, principalKind := range principalKinds {
				compatibleDefinitions = append(compatibleDefinitions, definition)
				compatiblePrincipalKinds = append(compatiblePrincipalKinds, principalKind)
			}
		}
		return queries.EvaluateCurrentPrescriptions(t.Context(), repo.EvaluateCurrentPrescriptionsParams{
			OrganizationID: organizationID, ResourceKind: "tool", ResourceKey: resource,
			DefinitionKeys: definitions, PrincipalKinds: principalKinds, PrincipalKeys: principalKeys,
			CompatibleDefinitionKeys: compatibleDefinitions, CompatiblePrincipalKinds: compatiblePrincipalKinds,
		})
	}

	insert(evaluationFixture{ID: evaluationUUID(1), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:interval", Version: 1, State: "active", Scope: "selected", StartsAt: future, ExpiresAt: nil, ActivatedAt: &activated, ExternalNote: "Scheduled.", Resources: []string{"tool:interval"}})
	insert(evaluationFixture{ID: evaluationUUID(2), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:interval", Version: 1, State: "active", Scope: "selected", StartsAt: older, ExpiresAt: &databaseNow, ActivatedAt: &activated, ExternalNote: "Expired exactly.", Resources: []string{"tool:interval"}})
	_, err := evaluate([]string{"user"}, []string{"user:interval"}, []string{"block-tools"}, "tool:interval")
	require.ErrorIs(t, err, pgx.ErrNoRows)

	insert(evaluationFixture{ID: evaluationUUID(3), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:dynamic", Version: 1, State: "active", Scope: "all", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Dynamic all."})
	row, err := evaluate([]string{"user"}, []string{"user:dynamic"}, []string{"block-tools"}, "tool:created-after-activation")
	require.NoError(t, err)
	require.Equal(t, "Dynamic all.", row.ExternalNote)

	insert(evaluationFixture{ID: evaluationUUID(24), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:immediate", Version: 1, State: "active", Scope: "selected", Immediate: true, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Immediate.", Resources: []string{"tool:immediate"}})
	row, err = evaluate([]string{"user"}, []string{"user:immediate"}, []string{"block-tools"}, "tool:immediate")
	require.NoError(t, err)
	require.Equal(t, "Immediate.", row.ExternalNote)

	insert(evaluationFixture{ID: evaluationUUID(4), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:scope", Version: 1, State: "active", Scope: "all", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &newerActivation, ExternalNote: "All fallback."})
	insert(evaluationFixture{ID: evaluationUUID(5), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:scope", Version: 1, State: "active", Scope: "selected", StartsAt: older, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Exact selected note.", Resources: []string{"tool:scope"}})
	row, err = evaluate([]string{"user"}, []string{"user:scope"}, []string{"block-tools"}, "tool:scope")
	require.NoError(t, err)
	require.Equal(t, "Exact selected note.", row.ExternalNote)

	insert(evaluationFixture{ID: evaluationUUID(6), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:fallback", Version: 1, State: "active", Scope: "all", StartsAt: older, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Still active all."})
	insert(evaluationFixture{ID: evaluationUUID(7), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:fallback", Version: 1, State: "active", Scope: "selected", StartsAt: older, ExpiresAt: &databaseNow, ActivatedAt: &newerActivation, ExternalNote: "Expired selected.", Resources: []string{"tool:fallback"}})
	row, err = evaluate([]string{"user"}, []string{"user:fallback"}, []string{"block-tools"}, "tool:fallback")
	require.NoError(t, err)
	require.Equal(t, "Still active all.", row.ExternalNote)

	insert(evaluationFixture{ID: evaluationUUID(8), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:definition", Version: 1, State: "active", Scope: "selected", StartsAt: older, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "First definition.", Resources: []string{"tool:definition"}})
	insert(evaluationFixture{ID: evaluationUUID(9), DefinitionKey: "broad-tools", PrincipalKind: "user", PrincipalKey: "user:definition", Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &newerActivation, ExternalNote: "Second definition.", Resources: []string{"tool:definition"}})
	row, err = evaluate([]string{"user"}, []string{"user:definition"}, []string{"block-tools", "broad-tools"}, "tool:definition")
	require.NoError(t, err)
	require.Equal(t, "First definition.", row.ExternalNote)

	insert(evaluationFixture{ID: evaluationUUID(10), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:principal", Version: 1, State: "active", Scope: "selected", StartsAt: older, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Exact principal.", Resources: []string{"tool:principal"}})
	insert(evaluationFixture{ID: evaluationUUID(11), DefinitionKey: "block-tools", PrincipalKind: "service", PrincipalKey: "service:principal", Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &newerActivation, ExternalNote: "Broader principal.", Resources: []string{"tool:principal"}})
	insert(evaluationFixture{ID: evaluationUUID(23), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "service:principal", Version: 1, State: "active", Scope: "selected", StartsAt: databaseNow.Add(-time.Minute), ExpiresAt: &activeUntil, ActivatedAt: &newerActivation, ExternalNote: "Crossed principal tuple.", Resources: []string{"tool:principal"}})
	row, err = evaluate([]string{"user", "service"}, []string{"user:principal", "service:principal"}, []string{"block-tools"}, "tool:principal")
	require.NoError(t, err)
	require.Equal(t, "Exact principal.", row.ExternalNote)

	insert(evaluationFixture{ID: evaluationUUID(12), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:starts", Version: 1, State: "active", Scope: "selected", StartsAt: older, ExpiresAt: &activeUntil, ActivatedAt: &newerActivation, ExternalNote: "Older start.", Resources: []string{"tool:starts"}})
	insert(evaluationFixture{ID: evaluationUUID(13), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:starts", Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Newer start.", Resources: []string{"tool:starts"}})
	row, err = evaluate([]string{"user"}, []string{"user:starts"}, []string{"block-tools"}, "tool:starts")
	require.NoError(t, err)
	require.Equal(t, "Newer start.", row.ExternalNote)

	insert(evaluationFixture{ID: evaluationUUID(14), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:activation", Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Older activation.", Resources: []string{"tool:activation"}})
	insert(evaluationFixture{ID: evaluationUUID(15), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:activation", Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &newerActivation, ExternalNote: "Newer activation.", Resources: []string{"tool:activation"}})
	row, err = evaluate([]string{"user"}, []string{"user:activation"}, []string{"block-tools"}, "tool:activation")
	require.NoError(t, err)
	require.Equal(t, "Newer activation.", row.ExternalNote)

	insert(evaluationFixture{ID: evaluationUUID(17), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:uuid", Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Larger UUID.", Resources: []string{"tool:uuid"}})
	insert(evaluationFixture{ID: evaluationUUID(16), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:uuid", Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Smaller UUID.", Resources: []string{"tool:uuid"}})
	row, err = evaluate([]string{"user"}, []string{"user:uuid"}, []string{"block-tools"}, "tool:uuid")
	require.NoError(t, err)
	require.Equal(t, "Smaller UUID.", row.ExternalNote)

	currentID := evaluationUUID(18)
	insert(evaluationFixture{ID: currentID, DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:current", CurrentVersion: 2, Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &newerActivation, ExternalNote: "Old noncurrent version.", Resources: []string{"tool:current"}})
	insertEvaluationVersion(t, conn, evaluationFixture{ID: currentID, OrganizationID: organizationID, Version: 2, State: "active", Scope: "all", StartsAt: older, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Current version."})
	row, err = evaluate([]string{"user"}, []string{"user:current"}, []string{"block-tools"}, "tool:current")
	require.NoError(t, err)
	require.Equal(t, "Current version.", row.ExternalNote)

	insert(evaluationFixture{ID: evaluationUUID(20), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:members", Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Selected members.", Resources: []string{"tool:member-a", "tool:member-b"}})
	for _, resourceKey := range []string{"tool:member-a", "tool:member-b"} {
		row, err = evaluate([]string{"user"}, []string{"user:members"}, []string{"block-tools"}, resourceKey)
		require.NoError(t, err)
		require.Equal(t, "Selected members.", row.ExternalNote)
	}
	_, err = evaluate([]string{"user"}, []string{"user:members"}, []string{"block-tools"}, "tool:unrelated")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = queries.EvaluateCurrentPrescriptions(t.Context(), repo.EvaluateCurrentPrescriptionsParams{
		OrganizationID: organizationID, ResourceKind: "other", ResourceKey: "tool:member-a",
		DefinitionKeys: []string{"block-tools"}, PrincipalKinds: []string{"user"}, PrincipalKeys: []string{"user:members"},
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	inactiveID := evaluationUUID(21)
	insert(evaluationFixture{ID: inactiveID, DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:inactive", CurrentVersion: 2, Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Old active version.", Resources: []string{"tool:inactive"}})
	insertEvaluationVersion(t, conn, evaluationFixture{ID: inactiveID, OrganizationID: organizationID, Version: 2, State: "inactive", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Inactive current version.", Resources: []string{"tool:inactive"}})
	_, err = evaluate([]string{"user"}, []string{"user:inactive"}, []string{"block-tools"}, "tool:inactive")
	require.ErrorIs(t, err, pgx.ErrNoRows)

	insert(evaluationFixture{ID: evaluationUUID(22), DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:unbounded", Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: nil, ActivatedAt: &activated, ExternalNote: "Active without expiry.", Resources: []string{"tool:unbounded"}})
	row, err = evaluate([]string{"user"}, []string{"user:unbounded"}, []string{"block-tools"}, "tool:unbounded")
	require.NoError(t, err)
	require.Equal(t, "Active without expiry.", row.ExternalNote)

	otherOrganization := "org_" + uuid.NewString()
	insertOrganization(t, conn, otherOrganization)
	insertEvaluationFixture(t, conn, evaluationFixture{ID: evaluationUUID(19), OrganizationID: otherOrganization, DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:tenant", ResourceKind: "tool", CurrentVersion: 1, Version: 1, State: "active", Scope: "selected", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Other tenant.", Resources: []string{"tool:tenant"}})
	_, err = evaluate([]string{"user"}, []string{"user:tenant"}, []string{"block-tools"}, "tool:tenant")
	require.ErrorIs(t, err, pgx.ErrNoRows)

	insert(evaluationFixture{ID: evaluationUUID(25), DefinitionKey: "service-only", PrincipalKind: "user", PrincipalKey: "user:compatible", Version: 1, State: "active", Scope: "all", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Incompatible higher-ranked definition."})
	insert(evaluationFixture{ID: evaluationUUID(26), DefinitionKey: "user-only", PrincipalKind: "user", PrincipalKey: "user:compatible", Version: 1, State: "active", Scope: "all", StartsAt: past, ExpiresAt: &activeUntil, ActivatedAt: &activated, ExternalNote: "Compatible lower-ranked definition."})
	row, err = queries.EvaluateCurrentPrescriptions(t.Context(), repo.EvaluateCurrentPrescriptionsParams{
		OrganizationID: organizationID, ResourceKind: "tool", ResourceKey: "tool:compatible",
		DefinitionKeys: []string{"service-only", "user-only"}, PrincipalKinds: []string{"user"}, PrincipalKeys: []string{"user:compatible"},
		CompatibleDefinitionKeys: []string{"user-only"}, CompatiblePrincipalKinds: []string{"user"},
	})
	require.NoError(t, err)
	require.Equal(t, "Compatible lower-ranked definition.", row.ExternalNote)
}

func TestEvaluateCurrentPrescriptionsRepresentativePlan(t *testing.T) {
	conn, organizationID := newLifecycleDatabase(t, "killswitch_evaluator_plan")
	otherOrganizationID := "org_" + uuid.NewString()
	insertOrganization(t, conn, otherOrganizationID)
	_, err := conn.Exec(t.Context(), `
		INSERT INTO killswitch_prescriptions (organization_id, definition_key, principal_kind, principal_key, resource_kind, current_version)
		SELECT $1, 'block-tools', 'user', 'user:plan', 'tool', 1
		FROM generate_series(1, 256)
	`, organizationID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `
		INSERT INTO killswitch_prescriptions (organization_id, definition_key, principal_kind, principal_key, resource_kind, current_version)
		SELECT $1, 'block-tools', 'user', 'user:noise', 'tool', 1
		FROM generate_series(1, 4096)
	`, otherOrganizationID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `
		INSERT INTO killswitch_prescription_versions (
		  organization_id, prescription_id, version, state, resource_scope, starts_at, expires_at, activated_at, internal_note, external_note
		)
		SELECT organization_id, id, 1, 'active', 'selected',
		  clock_timestamp() - INTERVAL '1 hour', NULL, clock_timestamp() - INTERVAL '1 hour',
		  'private plan fixture', 'Plan fixture.'
		FROM killswitch_prescriptions
		WHERE organization_id IN ($1, $2) AND principal_key IN ('user:plan', 'user:noise')
	`, organizationID, otherOrganizationID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `
		INSERT INTO killswitch_prescription_version_resources (organization_id, prescription_id, version, resource_key)
		SELECT organization_id, id, 1,
		  CASE principal_key WHEN 'user:plan' THEN 'tool:plan' ELSE 'tool:noise' END
		FROM killswitch_prescriptions
		WHERE organization_id IN ($1, $2) AND principal_key IN ('user:plan', 'user:noise')
	`, organizationID, otherOrganizationID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), "ANALYZE killswitch_prescriptions, killswitch_prescription_versions, killswitch_prescription_version_resources")
	require.NoError(t, err)

	capture := &capturingEvaluationDB{DBTX: conn, query: "", args: nil}
	queries := repo.New(capture)
	_, err = queries.EvaluateCurrentPrescriptions(t.Context(), repo.EvaluateCurrentPrescriptionsParams{
		OrganizationID: organizationID, ResourceKind: "tool", ResourceKey: "tool:plan",
		DefinitionKeys: []string{"block-tools"}, PrincipalKinds: []string{"user"}, PrincipalKeys: []string{"user:plan"},
		CompatibleDefinitionKeys: []string{"block-tools"}, CompatiblePrincipalKinds: []string{"user"},
	})
	require.NoError(t, err)
	require.NotContains(t, capture.query, "generate_subscripts")

	query := capture.query
	if newline := strings.IndexByte(query, '\n'); newline >= 0 && strings.HasPrefix(query, "--") {
		query = query[newline+1:]
	}
	rows, err := conn.Query(t.Context(), "EXPLAIN (ANALYZE, BUFFERS) "+query, capture.args...)
	require.NoError(t, err)
	defer rows.Close()
	var planLines []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		planLines = append(planLines, line)
	}
	require.NoError(t, rows.Err())
	plan := strings.Join(planLines, "\n")
	t.Log("representative evaluator plan:\n" + plan)
	var principalCandidatePlan string
	for _, line := range planLines {
		if strings.Contains(line, "Function Scan on candidate_1") {
			principalCandidatePlan = line
			break
		}
	}
	require.NotEmpty(t, principalCandidatePlan)
	estimateMatch := regexp.MustCompile(`(?:^|\s)rows=([0-9]+)(?:\s|$)`).FindStringSubmatch(principalCandidatePlan)
	require.Len(t, estimateMatch, 2)
	estimatedRows, err := strconv.Atoi(estimateMatch[1])
	require.NoError(t, err)
	require.Equal(t, 1, estimatedRows)
	require.Contains(t, plan, "killswitch_prescriptions_evaluator_idx")
	require.Contains(t, plan, "killswitch_prescription_version_resources_lookup_idx")
}

func TestKillswitchEvaluationIntervalUsesExactDatabaseTimeBoundaries(t *testing.T) {
	conn, organizationID := newLifecycleDatabase(t, "killswitch_evaluator_interval_boundaries")
	capture := &capturingEvaluationDB{DBTX: conn, query: "", args: nil}
	_, err := repo.New(capture).EvaluateCurrentPrescriptions(t.Context(), repo.EvaluateCurrentPrescriptionsParams{
		OrganizationID: organizationID, ResourceKind: "tool", ResourceKey: "tool:boundary",
		DefinitionKeys: []string{"block-tools"}, PrincipalKinds: []string{"user"}, PrincipalKeys: []string{"user:boundary"},
		CompatibleDefinitionKeys: []string{"block-tools"}, CompatiblePrincipalKinds: []string{"user"},
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Contains(t, capture.query, "WITH evaluation_clock AS MATERIALIZED")
	require.Equal(t, 1, strings.Count(capture.query, "clock_timestamp()"))
	require.Contains(t, capture.query, "version.starts_at IS NULL OR version.starts_at <= evaluation_clock.database_now")
	require.Contains(t, capture.query, "version.expires_at > evaluation_clock.database_now")

	var startsAtBoundaryMatches, expiresAtBoundaryMatches bool
	require.NoError(t, conn.QueryRow(t.Context(), `
		WITH evaluation_clock AS MATERIALIZED (
		  SELECT clock_timestamp() AS database_now
		), boundaries AS (
		  SELECT database_now AS starts_at, database_now AS expires_at
		  FROM evaluation_clock
		)
		SELECT
		  boundaries.starts_at <= evaluation_clock.database_now,
		  boundaries.expires_at > evaluation_clock.database_now
		FROM boundaries
		CROSS JOIN evaluation_clock
	`).Scan(&startsAtBoundaryMatches, &expiresAtBoundaryMatches))
	require.True(t, startsAtBoundaryMatches)
	require.False(t, expiresAtBoundaryMatches)
}

func insertEvaluationFixture(t *testing.T, conn repo.DBTX, fixture evaluationFixture) {
	t.Helper()
	_, err := conn.Exec(t.Context(), `
		INSERT INTO killswitch_prescriptions (id, organization_id, definition_key, principal_kind, principal_key, resource_kind, current_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, fixture.ID, fixture.OrganizationID, fixture.DefinitionKey, fixture.PrincipalKind, fixture.PrincipalKey, fixture.ResourceKind, fixture.CurrentVersion)
	require.NoError(t, err)
	insertEvaluationVersion(t, conn, fixture)
}

func insertEvaluationVersion(t *testing.T, conn repo.DBTX, fixture evaluationFixture) {
	t.Helper()
	var startsAt any = fixture.StartsAt
	if fixture.Immediate {
		startsAt = nil
	}
	_, err := conn.Exec(t.Context(), `
		INSERT INTO killswitch_prescription_versions (
		  organization_id, prescription_id, version, state, resource_scope, starts_at, expires_at, activated_at, internal_note, external_note
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'private fixture note', $9)
	`, fixture.OrganizationID, fixture.ID, fixture.Version, fixture.State, fixture.Scope, startsAt, fixture.ExpiresAt, fixture.ActivatedAt, fixture.ExternalNote)
	require.NoError(t, err)
	for _, resource := range fixture.Resources {
		_, err = conn.Exec(t.Context(), `
			INSERT INTO killswitch_prescription_version_resources (organization_id, prescription_id, version, resource_key)
			VALUES ($1, $2, $3, $4)
		`, fixture.OrganizationID, fixture.ID, fixture.Version, resource)
		require.NoError(t, err)
	}
}

func evaluationUUID(value int) uuid.UUID {
	return uuid.MustParse(fmtEvaluationUUID(value))
}

func fmtEvaluationUUID(value int) string {
	const hexadecimal = "0123456789abcdef"
	return "00000000-0000-0000-0000-0000000000" + string([]byte{hexadecimal[value/16], hexadecimal[value%16]})
}
