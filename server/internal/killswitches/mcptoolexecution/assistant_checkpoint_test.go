package mcptoolexecution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestAssistantCheckpointDelegationAndNextCallActivation(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_assistant_checkpoint")
	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, false)
	insertMembership(t, conn, orgID, userID, false)
	projectID := insertProject(t, conn, orgID, "assistant-checkpoint", false)
	var assistantID uuid.UUID
	//nolint:glint // notestingrawsql: direct SQL keeps the checkpoint fixture local to this package
	err := conn.QueryRow(t.Context(), `
		INSERT INTO assistants (project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status)
		VALUES ($1, $2, 'Assistant', 'openai/test', '', 300, 1, 'active') RETURNING id
	`, projectID, orgID).Scan(&assistantID)
	require.NoError(t, err)

	checkpoint, err := NewAssistantCheckpoint(conn, time.Second, testenv.NewMeterProvider(t), testenv.NewLogger(t), enforcedRollout(orgID))
	require.NoError(t, err)
	ctx := mcpidentity.NewValidatorBoundary().StampDelegatedUser(t.Context(), userID)

	disposition, err := checkpoint.Evaluate(ctx, orgID, assistantID)
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())

	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID: uuid.New(), DefinitionKey: DefinitionKeyAIAccess, PrincipalKey: userID,
		ResourceKind: ResourceKindAssistant, Scope: "selected", Resources: []string{assistantID.String()},
		ExternalNote: "AI work is paused for this user.",
	})
	for range 2 {
		disposition, err = checkpoint.Evaluate(ctx, orgID, assistantID)
		require.NoError(t, err)
		require.Equal(t, killswitches.TransportDispositionMatchedDenial, disposition.Kind())
		note, ok := disposition.ExternalNote()
		require.True(t, ok)
		require.Equal(t, "AI work is paused for this user.", note)
	}

	_, err = conn.Exec(t.Context(), `UPDATE organization_user_relationships SET deleted_at = clock_timestamp() WHERE organization_id = $1 AND user_id = $2`, orgID, userID) //nolint:glint // notestingrawsql: direct SQL keeps the checkpoint fixture local to this package
	require.NoError(t, err)
	disposition, err = checkpoint.Evaluate(ctx, orgID, assistantID)
	require.Error(t, err)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
	_, hasNote := disposition.ExternalNote()
	require.False(t, hasNote, "membership failure must not use match language")
}

//nolint:paralleltest,tparallel // Direct SQL and sequential subtests keep the fixture deterministic.
func TestAssistantCheckpointRejectsCrossTenantDelegationAndPassesAssistantOnlyWork(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_assistant_checkpoint_invalid")
	projectID := insertProject(t, conn, orgID, "assistant-invalid", false)
	var assistantID uuid.UUID
	//nolint:glint // notestingrawsql: direct SQL and sequential subtests keep the fixture deterministic
	err := conn.QueryRow(t.Context(), `
		INSERT INTO assistants (project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status)
		VALUES ($1, $2, 'Assistant', 'openai/test', '', 300, 1, 'active') RETURNING id
	`, projectID, orgID).Scan(&assistantID)
	require.NoError(t, err)
	otherOrgID := "org-other"
	flags := enforcedRollout(orgID)
	flags.SetFlag(feature.FlagMCPKillswitchEnforce, otherOrgID, true)
	checkpoint, err := NewAssistantCheckpoint(conn, time.Second, testenv.NewMeterProvider(t), testenv.NewLogger(t), flags)
	otherUserID := "user-other"
	insertOrganization(t, conn, otherOrgID)
	insertUser(t, conn, otherUserID, false)
	insertMembership(t, conn, otherOrgID, otherUserID, false)
	require.NoError(t, err)

	for _, test := range []struct {
		name, organization string
		ctxKind            mcpidentity.Kind
		userID             string
	}{
		{name: "cross tenant", organization: "org-other", ctxKind: mcpidentity.KindDelegatedUser, userID: "user-other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			if test.ctxKind == mcpidentity.KindDelegatedUser {
				ctx = mcpidentity.NewValidatorBoundary().StampDelegatedUser(ctx, test.userID)
			}
			disposition, err := checkpoint.Evaluate(ctx, test.organization, assistantID)
			require.Error(t, err)
			require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
			_, hasNote := disposition.ExternalNote()
			require.False(t, hasNote)
		})
	}

	// Assistant-only work (triggers, MCP auth resumption) is not governed.
	for name, ctx := range map[string]context.Context{
		"unstamped":      t.Context(),
		"assistant-only": mcpidentity.NewValidatorBoundary().StampAssistant(t.Context()),
	} {
		disposition, err := checkpoint.Evaluate(ctx, orgID, assistantID)
		require.NoError(t, err, name)
		require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind(), name)
	}
}
