package mcptoolexecution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHostedInferenceCheckpointUsesRealAIAccessEvaluator(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_hosted_inference")
	registry, err := NewRegistry(conn)
	require.NoError(t, err)
	evaluation, err := killswitches.NewEvaluator(conn, registry, time.Second, nil, testenv.NewLogger(t))
	require.NoError(t, err)
	checkpoint, err := hostedinference.NewCheckpoint(registry, evaluation, time.Second)
	require.NoError(t, err)

	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	sessionID := "session"
	ctx := contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: orgID, UserID: userID, SessionID: &sessionID}, false)
	ctx, err = hostedinference.WithGovernedUser(ctx, hostedinference.CallCategoryUserChatCompletion)
	require.NoError(t, err)

	require.NoError(t, checkpoint.Check(ctx, orgID))
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID: uuid.New(), DefinitionKey: DefinitionKeyAIAccess, PrincipalKey: userID,
		ResourceKind: hostedinference.ResourceKindGramHostedInference, Scope: "selected",
		Resources:    []string{string(hostedinference.CallCategoryUserChatCompletion)},
		ExternalNote: "  Hosted inference paused exactly.  ",
	})

	err = checkpoint.Check(ctx, orgID)
	var denial *hostedinference.MatchedDenialError
	require.ErrorAs(t, err, &denial)
	require.Equal(t, "hosted inference access denied", denial.Error())
	require.Equal(t, "Hosted inference paused exactly.", denial.ExternalNote())

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	err = checkpoint.Check(canceled, orgID)
	var unavailable *hostedinference.InfrastructureUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.NotContains(t, err.Error(), "Hosted inference paused")
}
