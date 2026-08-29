package mcptoolexecution

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

//nolint:glint // Direct SQL keeps the checkpoint fixture local to this package.
func TestAssistantCheckpointDelegationAndNextCallActivation(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_assistant_checkpoint")
	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	projectID := insertProject(t, conn, orgID, "assistant-checkpoint", nil)
	var assistantID uuid.UUID
	err := conn.QueryRow(t.Context(), `
		INSERT INTO assistants (project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status)
		VALUES ($1, $2, 'Assistant', 'openai/test', '', 300, 1, 'active') RETURNING id
	`, projectID, orgID).Scan(&assistantID)
	require.NoError(t, err)

	checkpoint, err := NewAssistantCheckpoint(conn, time.Second, testenv.NewMeterProvider(t), testenv.NewLogger(t))
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

	_, err = conn.Exec(t.Context(), `UPDATE organization_user_relationships SET deleted_at = clock_timestamp() WHERE organization_id = $1 AND user_id = $2`, orgID, userID)
	require.NoError(t, err)
	disposition, err = checkpoint.Evaluate(ctx, orgID, assistantID)
	require.Error(t, err)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
	_, hasNote := disposition.ExternalNote()
	require.False(t, hasNote, "membership failure must not use match language")
}

//nolint:glint,paralleltest,tparallel // Direct SQL and sequential subtests keep the fixture deterministic.
func TestAssistantCheckpointRejectsMissingAndCrossTenantDelegationWithoutMatchNote(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_assistant_checkpoint_invalid")
	projectID := insertProject(t, conn, orgID, "assistant-invalid", nil)
	var assistantID uuid.UUID
	err := conn.QueryRow(t.Context(), `
		INSERT INTO assistants (project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status)
		VALUES ($1, $2, 'Assistant', 'openai/test', '', 300, 1, 'active') RETURNING id
	`, projectID, orgID).Scan(&assistantID)
	require.NoError(t, err)
	checkpoint, err := NewAssistantCheckpoint(conn, time.Second, testenv.NewMeterProvider(t), testenv.NewLogger(t))
	require.NoError(t, err)

	for _, test := range []struct {
		name, organization string
		ctxKind            mcpidentity.Kind
		userID             string
	}{
		{name: "missing", organization: orgID},
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
}
