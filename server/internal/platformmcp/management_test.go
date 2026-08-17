package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOnboardingRegistrationIdempotencyKeyIsBoundedAndTargetStable(t *testing.T) {
	t.Parallel()

	workflowID := uuid.New()
	providerKey := "browser-catalog-registry-" + uuid.NewString() + "-with-a-long-provider-key"
	catalogRef := "com.example.registry/very-long-catalogue-reference/linear/latest"

	first := onboardingRegistrationIdempotencyKey(workflowID, "project", providerKey, catalogRef)
	second := onboardingRegistrationIdempotencyKey(workflowID, "project", providerKey, catalogRef)

	require.Equal(t, first, second)
	require.LessOrEqual(t, len(first), 128)
	require.NotEqual(t, first, onboardingRegistrationIdempotencyKey(workflowID, "other-project", providerKey, catalogRef))
	require.NotEqual(t, first, onboardingRegistrationIdempotencyKey(workflowID, "project", providerKey, "other/reference"))
}

func TestAgentConfigurationReady(t *testing.T) {
	t.Parallel()

	copiedAt := time.Now().UTC()
	tests := []struct {
		name       string
		projection OnboardingProjection
		want       bool
	}{
		{
			name: "does not credit an unopened workflow",
			want: false,
		},
		{
			name:       "does not credit client selection alone",
			projection: OnboardingProjection{Workflow: &OnboardingWorkflow{}},
			want:       false,
		},
		{
			name:       "credits explicit configuration copy",
			projection: OnboardingProjection{Workflow: &OnboardingWorkflow{AgentConfigurationCopiedAt: &copiedAt}},
			want:       true,
		},
		{
			name: "credits an authenticated manual configuration",
			projection: OnboardingProjection{
				Workflow:            &OnboardingWorkflow{},
				Connections:         []OnboardingConnection{{ID: uuid.New()}},
				ConnectionAuthState: ConnectionAuthStateActive,
			},
			want: true,
		},
		{
			name: "does not credit terminal connection evidence",
			projection: OnboardingProjection{
				Workflow:            &OnboardingWorkflow{},
				EvidenceConnection:  &OnboardingConnection{ID: uuid.New(), Ready: true},
				ConnectionAuthState: ConnectionAuthStateReauthorizationRequired,
			},
			want: false,
		},
		{
			name:       "credits later lifecycle evidence",
			projection: OnboardingProjection{Workflow: &OnboardingWorkflow{}, CatalogExplored: true},
			want:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, agentConfigurationReady(test.projection))
		})
	}
}
