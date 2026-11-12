package adminmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// seedKitReadyAssistant creates an assistant with a dedicated agent in a
// project that has a shared user-session issuer.
func seedKitReadyAssistant(t *testing.T, f writeFixture, organizationID string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	project, assistant := seedLegacyAssistant(t, f, organizationID)
	row, err := assistantrepo.New(f.db).GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: project, AssistantID: assistant})
	require.NoError(t, err)
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary only; provisioning uses generated SQLc methods.
	require.NoError(t, err)
	require.NoError(t, assistantidentity.New("https://platform.example.invalid", audit.NewLogger()).Provision(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: organizationID, ProjectID: project, AssistantID: assistant, ActorUserID: row.CreatedByUserID.String, Staff: nil}))
	require.NoError(t, tx.Commit(t.Context()))
	_, err = usersessionsrepo.New(f.db).CreateUserSessionIssuer(t.Context(), usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID: project, OrganizationID: pgtype.Text{String: organizationID, Valid: true}, Slug: "shared-" + uuid.NewString()[:8],
		AuthnChallengeMode: "chain", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		AuthorizationServerMode: pgtype.Text{String: "shared", Valid: true}, UseAuthenticationHost: pgtype.Bool{},
	})
	require.NoError(t, err)
	return project, assistant
}

func TestAssistantKitMigrationApplies(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_assistant_kit")
	writer := &assistantKitMigrationWriter{runner: f.runner, audit: audit.NewLogger(), writes: WriteConfig{Enabled: true}, now: time.Now}
	ctx := writeContext(t, f)
	project, assistant := seedKitReadyAssistant(t, f, f.orgA)
	foreignProject, foreign := seedKitReadyAssistant(t, f, f.orgB)

	receipt, err := writer.apply(ctx, MigrateAssistantsToKitInput{OrganizationID: f.orgA, AssistantIDs: nil})
	require.NoError(t, err)
	require.Equal(t, int64(1), receipt.Switched)
	require.Equal(t, []assistantKitMigrationResult{{AssistantID: assistant.String(), ProjectID: project.String(), Outcome: "switched"}}, receipt.Results)
	row, err := assistantrepo.New(f.db).GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: project, AssistantID: assistant})
	require.NoError(t, err)
	require.Equal(t, int16(3), row.RuntimeVersion)
	switched, err := audittest.LatestAuditLogByActionAndSubject(t.Context(), f.db, audit.ActionAssistantRuntimeSwitch, assistant.String())
	require.NoError(t, err)
	require.Equal(t, new(string(audit.SurfaceAdminMCP)), switched.ActingSurface)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationMigrateAssistantsToKit, f.orgA, "executed"))

	again, err := writer.apply(ctx, MigrateAssistantsToKitInput{OrganizationID: f.orgA, AssistantIDs: []string{assistant.String()}})
	require.NoError(t, err)
	require.Zero(t, again.Switched)
	require.Equal(t, int64(1), again.AlreadyKit)

	_, err = writer.apply(ctx, MigrateAssistantsToKitInput{OrganizationID: f.orgA, AssistantIDs: []string{foreign.String()}})
	require.ErrorContains(t, err, "not a live assistant of the organization")
	foreignRow, err := assistantrepo.New(f.db).GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: foreignProject, AssistantID: foreign})
	require.NoError(t, err)
	require.Equal(t, int16(2), foreignRow.RuntimeVersion)

	_, err = writer.apply(ctx, MigrateAssistantsToKitInput{OrganizationID: "org_missing_" + uuid.NewString(), AssistantIDs: nil})
	require.ErrorContains(t, err, "no organization with that ID")
}
