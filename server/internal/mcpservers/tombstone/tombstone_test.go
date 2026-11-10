package tombstone

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAuditActorPrefersTheUserOverActorSystem(t *testing.T) {
	t.Parallel()

	both := Input{ActorUserID: "user_1", ActorSystem: "cleanup"}
	require.True(t, hasActor(t.Context(), both))
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeUser, "user_1"), auditActor(t.Context(), both))

	system := Input{ActorSystem: "cleanup"}
	require.True(t, hasActor(t.Context(), system))
	require.Equal(t, urn.NewSystemPrincipal("cleanup"), auditActor(t.Context(), system))
}

func TestEmptyActorSystemKeepsTheProductActor(t *testing.T) {
	t.Parallel()

	user := Input{ActorUserID: "user_1"}
	require.True(t, hasActor(t.Context(), user))
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeUser, "user_1"), auditActor(t.Context(), user))

	require.False(t, hasActor(t.Context(), Input{}), "no user, agent or system actor is still rejected")
}

func TestAuditActorChoosesTheAuthenticatedAgent(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())
	agentCtx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ProjectID: &projectID}, agent)
	require.True(t, hasActor(agentCtx, Input{}))
	require.Equal(t, agent, auditActor(agentCtx, Input{}))
	require.Equal(t, agent, auditActor(agentCtx, Input{ActorSystem: "cleanup"}), "the audit logger records the agent over a system actor")
}
