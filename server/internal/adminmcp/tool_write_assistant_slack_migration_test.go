package adminmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func TestAssistantSlackMigrationAppliesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_assistant_slack_migration")
	enc := testenv.NewEncryptionClient(t)

	project, assistant := seedLegacyAssistant(t, f, f.orgA)
	foreignProject, foreignAssistant := seedLegacyAssistant(t, f, f.orgB)

	identities := &assistantIdentityMigrationWriter{
		runner:     f.runner,
		identities: assistantidentity.New("https://platform.example.invalid", audit.NewLogger()),
		writes:     WriteConfig{Enabled: true},
	}
	ctx := writeContext(t, f)
	_, err := identities.apply(ctx, MigrateAssistantIdentitiesInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	_, err = identities.apply(ctx, MigrateAssistantIdentitiesInput{OrganizationID: f.orgB})
	require.NoError(t, err)

	seedTrigger := func(orgID string, projID, assistantID uuid.UUID, secret string) uuid.UUID {
		envRow, envErr := envrepo.New(f.db).CreateEnvironment(t.Context(), envrepo.CreateEnvironmentParams{
			OrganizationID: orgID,
			ProjectID:      projID,
			Name:           "Slack Env " + uuid.NewString()[:8],
			Slug:           "slack-env-" + uuid.NewString()[:8],
			Description:    pgtype.Text{String: "", Valid: false},
		})
		require.NoError(t, envErr)
		cipher, encErr := enc.Encrypt([]byte(secret))
		require.NoError(t, encErr)
		_, envErr = envrepo.New(f.db).UpsertEnvironmentEntry(t.Context(), envrepo.UpsertEnvironmentEntryParams{
			Name:          authclients.SlackHeaderSigningSecret,
			Value:         cipher,
			IsSecret:      true,
			EnvironmentID: envRow.ID,
			ProjectID:     projID,
		})
		require.NoError(t, envErr)
		trig, trigErr := triggerrepo.New(f.db).CreateTriggerInstance(t.Context(), triggerrepo.CreateTriggerInstanceParams{
			OrganizationID: orgID,
			ProjectID:      projID,
			DefinitionSlug: "slack",
			Name:           "Slack",
			EnvironmentID:  uuid.NullUUID{UUID: envRow.ID, Valid: true},
			AuthClientID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			TargetKind:     "assistant",
			TargetRef:      assistantID.String(),
			TargetDisplay:  "Legacy",
			ConfigJson:     []byte(`{"routing":"threads"}`),
			Status:         "active",
		})
		require.NoError(t, trigErr)
		return trig.ID
	}

	const rawSecret = "super-secret-signing-value-never-leaked"
	trigA := seedTrigger(f.orgA, project, assistant, rawSecret)
	trigB := seedTrigger(f.orgB, foreignProject, foreignAssistant, "foreign-secret")

	writer := &assistantSlackMigrationWriter{runner: f.runner, audit: audit.NewLogger(), enc: enc, writes: WriteConfig{Enabled: true}}
	receipt, err := writer.apply(ctx, MigrateAssistantSlackInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Equal(t, f.orgA, receipt.OrganizationID)
	require.Equal(t, int64(1), receipt.Migrated)
	require.Zero(t, receipt.AlreadyMigrated)
	require.Zero(t, receipt.NoSlack)
	require.Zero(t, receipt.NeedsIdentity)
	require.Zero(t, receipt.Failed)
	require.Len(t, receipt.Results, 1)
	require.Equal(t, assistant.String(), receipt.Results[0].AssistantID)
	require.Equal(t, "migrated", receipt.Results[0].Outcome)
	require.NotNil(t, receipt.Results[0].AuthClientID)
	require.Equal(t, int64(1), receipt.Results[0].TriggersRepointed)

	// Receipt never contains secret plaintext.
	rawReceipt, err := json.Marshal(receipt)
	require.NoError(t, err)
	require.NotContains(t, string(rawReceipt), rawSecret)

	// Trigger A repointed; foreign Trigger B untouched.
	gotA, err := triggerrepo.New(f.db).GetTriggerInstanceByIDPublic(t.Context(), trigA)
	require.NoError(t, err)
	require.True(t, gotA.AuthClientID.Valid)
	require.False(t, gotA.EnvironmentID.Valid)

	gotB, err := triggerrepo.New(f.db).GetTriggerInstanceByIDPublic(t.Context(), trigB)
	require.NoError(t, err)
	require.False(t, gotB.AuthClientID.Valid)
	require.True(t, gotB.EnvironmentID.Valid)

	// Audit log attributes mutation to admin_mcp surface and records write event.
	entry, err := audittest.LatestAuditLogByActionAndSubject(t.Context(), f.db, audit.ActionAuthClientCreate, *receipt.Results[0].AuthClientID)
	require.NoError(t, err)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.Equal(t, new(string(audit.SurfaceAdminMCP)), entry.ActingSurface)
	require.Equal(t, new("test-client"), entry.ActingClientID)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationMigrateAssistantSlack, f.orgA, writeEventExecuted))

	// Re-run is idempotent.
	again, err := writer.apply(ctx, MigrateAssistantSlackInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Zero(t, again.Migrated)
	require.Equal(t, int64(1), again.AlreadyMigrated)
	require.Equal(t, "already_migrated", again.Results[0].Outcome)

	_, err = writer.apply(ctx, MigrateAssistantSlackInput{OrganizationID: "org_missing_" + uuid.NewString()})
	require.ErrorContains(t, err, "no organization with that ID")
}

func TestAssistantSlackMigrationRequiresAuthorityAndExactTarget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		writes         WriteConfig
		ctx            context.Context //nolint:containedctx // Table-driven authority contexts are scoped to this test.
		organizationID string
		want           error
	}{
		{"disabled", WriteConfig{}, t.Context(), "org_synthetic", ErrWriteDisabled},
		{"unauthenticated", WriteConfig{Enabled: true}, t.Context(), "org_synthetic", ErrWriteIdentity},
		{"read only", WriteConfig{Enabled: true}, writePrincipalContext(t, []string{ScopeRead}), "org_synthetic", ErrWriteScope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			writer := &assistantSlackMigrationWriter{writes: tc.writes}
			_, err := writer.apply(tc.ctx, MigrateAssistantSlackInput{OrganizationID: tc.organizationID})
			require.ErrorIs(t, err, tc.want)
		})
	}
	writer := &assistantSlackMigrationWriter{writes: WriteConfig{Enabled: true}}
	for _, id := range []string{"", " org_synthetic", "org_synthetic "} {
		_, err := writer.apply(writePrincipalContext(t, []string{ScopeRead, ScopeWrite}), MigrateAssistantSlackInput{OrganizationID: id})
		require.ErrorContains(t, err, "provide an exact organization ID")
	}
	require.Contains(t, adminServerInstructions, string(OperationMigrateAssistantSlack))
}

func TestAssistantSlackMigrationReceiptBounded(t *testing.T) {
	t.Parallel()
	results := make([]*gen.AdminAssistantSlackMigrationResult, maxAssistantSlackMigrationResults+5)
	for i := range results {
		results[i] = &gen.AdminAssistantSlackMigrationResult{
			AssistantID:       fmt.Sprintf("assistant-%d", i),
			ProjectID:         "project-1",
			Name:              "Assistant",
			Outcome:           "migrated",
			TriggersRepointed: 1,
		}
	}
	receipt := assistantSlackMigrationReceiptFromRun(&gen.AdminAssistantSlackMigrationRun{
		OrganizationID: "org_synthetic",
		Migrated:       int64(len(results)),
		Results:        results,
	})
	require.True(t, receipt.PossiblyIncomplete)
	require.Len(t, receipt.Results, maxAssistantSlackMigrationResults)
	require.Equal(t, int64(len(results)), receipt.Migrated)
}
