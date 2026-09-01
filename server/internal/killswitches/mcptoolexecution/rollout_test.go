package mcptoolexecution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type failingRolloutProvider struct{ feature.InMemory }

func (*failingRolloutProvider) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, errors.New("local flag state unavailable")
}

func TestResolveRolloutMode(t *testing.T) {
	t.Parallel()

	const organizationID = "org_test"
	flags := &feature.InMemory{}

	mode, err := ResolveRolloutMode(t.Context(), nil, organizationID)
	require.NoError(t, err)
	require.Equal(t, RolloutModeOff, mode)

	mode, err = ResolveRolloutMode(t.Context(), flags, organizationID)
	require.NoError(t, err)
	require.Equal(t, RolloutModeOff, mode)

	flags.SetFlag(feature.FlagMCPKillswitchShadow, organizationID, true)
	mode, err = ResolveRolloutMode(t.Context(), flags, organizationID)
	require.NoError(t, err)
	require.Equal(t, RolloutModeShadow, mode)

	flags.SetFlag(feature.FlagMCPKillswitchEnforce, organizationID, true)
	mode, err = ResolveRolloutMode(t.Context(), flags, organizationID)
	require.NoError(t, err)
	require.Equal(t, RolloutModeEnforce, mode)

	mode, err = ResolveRolloutMode(t.Context(), &failingRolloutProvider{}, organizationID)
	require.Error(t, err)
	require.Equal(t, RolloutModeOff, mode)
}

func TestEvaluateForRolloutFailsClosedWithoutEvaluation(t *testing.T) {
	t.Parallel()

	called := false
	disposition, err := evaluateForRollout(t.Context(), &failingRolloutProvider{}, "org_test", func() (killswitches.TransportDisposition, error) {
		called = true
		return killswitches.NewContinueDisposition(), nil
	})
	require.ErrorContains(t, err, "local flag state unavailable")
	require.False(t, called)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
}

func TestCheckpointTransitionsFromOffToShadowToEnforce(t *testing.T) {
	t.Parallel()

	conn, orgID := newTestDatabase(t, "ks_mcp_rollout")
	registry, err := NewRegistry(conn)
	require.NoError(t, err)
	realEvaluator, err := killswitches.NewEvaluator(conn, registry, time.Second, nil, testenv.NewLogger(t))
	require.NoError(t, err)
	counted := &countingEvaluator{delegate: realEvaluator}
	flags := &feature.InMemory{}
	checkpoint, err := newCheckpoint(registry, counted, time.Second, flags)
	require.NoError(t, err)

	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	projectID := insertProject(t, conn, orgID, "rollout", nil)
	serverID := insertMCPServer(t, conn, orgID, projectID, nil)
	insertPrescription(t, conn, orgID, prescriptionFixture{
		ID: uuid.New(), PrincipalKey: userID, Scope: "all", ExternalNote: "Paused during rollout.",
	})
	ctx := testIdentityContext(t, mcpidentity.KindUserSession, userID)

	disposition, err := checkpoint.Evaluate(ctx, orgID, serverID.String())
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())
	require.Zero(t, counted.calls, "off mode must not query the evaluator")

	flags.SetFlag(feature.FlagMCPKillswitchShadow, orgID, true)
	disposition, err = checkpoint.Evaluate(ctx, orgID, serverID.String())
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())
	require.Equal(t, 1, counted.calls, "shadow mode must evaluate without denying")

	flags.SetFlag(feature.FlagMCPKillswitchEnforce, orgID, true)
	disposition, err = checkpoint.Evaluate(ctx, orgID, serverID.String())
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionMatchedDenial, disposition.Kind())
	require.Equal(t, 2, counted.calls)
}
