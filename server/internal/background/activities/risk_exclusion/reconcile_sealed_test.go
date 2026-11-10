package risk_exclusion_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_exclusion"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// sealedFindings records one open finding per match and seals their evidence
// under the tenant's risk-evidence key, returning the finding ids in order.
func sealedFindings(t *testing.T, conn *pgxpool.Pool, tenant testTenant, cipher *risk.ResultEvidenceCipher, matches ...string) []uuid.UUID {
	t.Helper()
	ctx := t.Context()
	q := riskrepo.New(conn)

	policy, err := q.CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             uuid.Must(uuid.NewV7()),
		ProjectID:      tenant.projectID,
		OrganizationID: tenant.orgID,
		Name:           "sealed findings policy",
		Sources:        []string{"gitleaks"},
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
		AutoName:       false,
	})
	require.NoError(t, err)

	chatID, err := q.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: tenant.projectID, OrganizationID: tenant.orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)

	ids := make([]uuid.UUID, 0, len(matches))
	rows := make([]riskrepo.InsertRiskResultsParams, 0, len(matches))
	for _, match := range matches {
		msgID, err := q.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
			ChatID: chatID, ProjectID: uuid.NullUUID{UUID: tenant.projectID, Valid: true}, Content: match,
			UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
		})
		require.NoError(t, err)

		id := uuid.Must(uuid.NewV7())
		ids = append(ids, id)
		rows = append(rows, riskrepo.InsertRiskResultsParams{
			ID:                id,
			ProjectID:         tenant.projectID,
			OrganizationID:    tenant.orgID,
			RiskPolicyID:      policy.ID,
			RiskPolicyVersion: policy.Version,
			ChatMessageID:     uuid.NullUUID{UUID: msgID, Valid: true},
			Source:            "gitleaks",
			Found:             true,
			RuleID:            pgtype.Text{String: "secret.generic", Valid: true},
			Match:             pgtype.Text{String: match, Valid: true},
		})
	}
	_, err = q.InsertRiskResults(ctx, rows)
	require.NoError(t, err)

	report, err := risk.BackfillResultEvidence(ctx, testenv.NewLogger(t), conn, cipher, tenant.orgID, 100)
	require.NoError(t, err)
	require.Equal(t, int64(len(matches)), report.Sealed)

	return ids
}

func excludedBy(t *testing.T, conn *pgxpool.Pool, projectID uuid.UUID) map[uuid.UUID]uuid.NullUUID {
	t.Helper()

	rows, err := riskrepo.New(conn).ListRiskResultEvidenceForTest(t.Context(), projectID)
	require.NoError(t, err)
	byID := make(map[uuid.UUID]uuid.NullUUID, len(rows))
	for _, row := range rows {
		require.False(t, row.Match.Valid, "every finding here is sealed")
		byID[row.ID] = row.ExcludedExclusionID
	}

	return byID
}

func createExclusion(t *testing.T, conn *pgxpool.Pool, tenant testTenant, matchType, matchValue string) riskrepo.RiskExclusion {
	t.Helper()

	exclusion, err := riskrepo.New(conn).CreateRiskExclusion(t.Context(), riskrepo.CreateRiskExclusionParams{
		ProjectID:      tenant.projectID,
		OrganizationID: tenant.orgID,
		RiskPolicyID:   uuid.NullUUID{},
		MatchType:      matchType,
		MatchValue:     matchValue,
		RuleIDFilter:   pgtype.Text{},
		SourceFilter:   pgtype.Text{},
		Enabled:        true,
	})
	require.NoError(t, err)

	return exclusion
}

// reconcileWithoutClickHouse reconciles the Postgres phases only.
func reconcileWithoutClickHouse(t *testing.T, conn *pgxpool.Pool, cipher *risk.ResultEvidenceCipher, tenant testTenant, exclusionID uuid.UUID) {
	t.Helper()

	reconcile := risk_exclusion.NewReconcile(
		testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t),
		conn, nil, risk.Fingerprinter{}, cipher, nil, true,
	)
	require.NoError(t, runReconcile(t, reconcile, risk_exclusion.ReconcileArgs{
		ProjectID:   tenant.projectID,
		ExclusionID: exclusionID,
		WindowDays:  0,
	}))
}

func TestReconcile_ExactAndRegexExclusionsFlagSealedMatches(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	tenant := seedTenant(t, conn)
	fixture := orgencryptiontest.New(t, conn)
	require.NoError(t, fixture.Provisioner.EnsureUseCase(t.Context(), tenant.orgID, orgencryption.UseCaseRiskEvidence, orgencryption.SystemActor()))
	cipher := risk.NewResultEvidenceCipher(fixture.Service)

	ids := sealedFindings(t, conn, tenant, cipher, "AKIAIOSFODNN7EXAMPLE", "token-12345", "unrelated value")
	akia, token, unrelated := ids[0], ids[1], ids[2]

	exact := createExclusion(t, conn, tenant, "exact", "AKIAIOSFODNN7EXAMPLE")
	reconcileWithoutClickHouse(t, conn, cipher, tenant, exact.ID)

	flags := excludedBy(t, conn, tenant.projectID)
	require.Equal(t, uuid.NullUUID{UUID: exact.ID, Valid: true}, flags[akia])
	require.False(t, flags[token].Valid)
	require.False(t, flags[unrelated].Valid)

	regex := createExclusion(t, conn, tenant, "regex", `^token-\d+$`)
	reconcileWithoutClickHouse(t, conn, cipher, tenant, regex.ID)

	flags = excludedBy(t, conn, tenant.projectID)
	require.Equal(t, uuid.NullUUID{UUID: exact.ID, Valid: true}, flags[akia])
	require.Equal(t, uuid.NullUUID{UUID: regex.ID, Valid: true}, flags[token])
	require.False(t, flags[unrelated].Valid)
}

func TestReconcile_SealedMatchesStayUnflaggedWhenTheyCannotBeOpened(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	tenant := seedTenant(t, conn)
	fixture := orgencryptiontest.New(t, conn)
	require.NoError(t, fixture.Provisioner.EnsureUseCase(t.Context(), tenant.orgID, orgencryption.UseCaseRiskEvidence, orgencryption.SystemActor()))
	cipher := risk.NewResultEvidenceCipher(fixture.Service)

	ids := sealedFindings(t, conn, tenant, cipher, "AKIAIOSFODNN7EXAMPLE")
	exact := createExclusion(t, conn, tenant, "exact", "AKIAIOSFODNN7EXAMPLE")

	reconcileWithoutClickHouse(t, conn, nil, tenant, exact.ID)
	require.False(t, excludedBy(t, conn, tenant.projectID)[ids[0]].Valid, "a worker without organization encryption cannot evaluate sealed matches")

	require.NoError(t, fixture.Provisioner.Suspend(t.Context(), tenant.orgID, "customer disabled the key", orgencryption.SystemActor()))
	reconcileWithoutClickHouse(t, conn, cipher, tenant, exact.ID)
	require.False(t, excludedBy(t, conn, tenant.projectID)[ids[0]].Valid, "a suspended key leaves sealed matches unflagged")
}
