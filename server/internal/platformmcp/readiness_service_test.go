package platformmcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRepairActionsAreBoundedAndStateSpecific(t *testing.T) {
	t.Parallel()

	states := []ReadinessState{
		ReadinessReady,
		ReadinessNeedsProviderSetup,
		ReadinessNeedsGramAuthorization,
		ReadinessNeedsConfiguration,
		ReadinessAuthFailed,
		ReadinessUnreachable,
		ReadinessUnsupported,
		ReadinessUnauthorized,
		ReadinessGuideUnavailable,
		ReadinessDegraded,
	}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			actions := repairActions(state)
			require.LessOrEqual(t, len(actions), 3)
			if state == ReadinessReady {
				require.Empty(t, actions)
				return
			}
			require.NotEmpty(t, actions)
			for _, action := range actions {
				require.NotEmpty(t, action.Kind)
				require.NotEmpty(t, action.Label)
			}
		})
	}
}

func TestReadinessFreshnessIsSeparateFromState(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	fresh := Readiness{State: ReadinessReady, ExpiresAt: now.Add(time.Minute), Fresh: true}
	stale := Readiness{State: ReadinessReady, ExpiresAt: now.Add(-time.Minute), Fresh: false}

	require.Equal(t, "fresh", readinessFreshness(fresh, true))
	require.Equal(t, "stale", readinessFreshness(stale, true))
	require.Equal(t, "unavailable", readinessFreshness(Readiness{}, false))
	require.Equal(t, ReadinessReady, stale.State)
}

func TestMissingReadinessNormalizesToAStableRepairProjection(t *testing.T) {
	t.Parallel()

	readiness := normalizedReadiness(Readiness{}, false)
	output := readinessToolOutput("project", "registration", readiness, false)

	require.Equal(t, ReadinessDegraded, readiness.State)
	require.Equal(t, "readiness_unavailable", readiness.EvidenceCode)
	require.Equal(t, "unavailable", output.Freshness)
	require.Equal(t, []RepairAction{{Kind: "retry_readiness", Label: "Retry the authenticated readiness check"}}, output.Actions)
}

func TestReadinessToolOutputDoesNotExposeProviderAuthorizationIdentity(t *testing.T) {
	t.Parallel()

	output := readinessToolOutput("project", "registration", Readiness{
		State:        ReadinessUnauthorized,
		EvidenceCode: "provider_authorization_rejected",
		CheckedAt:    time.Date(2026, time.August, 7, 0, 0, 0, 0, time.UTC),
		ExpiresAt:    time.Date(2026, time.August, 7, 0, 1, 0, 0, time.UTC),
		Fresh:        true,
	}, true)

	require.Equal(t, "project", output.ProjectSlug)
	require.Equal(t, "registration", output.RegistrationID)
	require.Equal(t, ReadinessUnauthorized, output.State)
	require.Equal(t, "fresh", output.Freshness)
	require.NotEmpty(t, output.Actions)
	require.Equal(t, "provider_authorization_rejected", output.EvidenceCode)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "provider_authorization_fingerprint")
	require.NotContains(t, string(encoded), "token")
	require.NotContains(t, string(encoded), "connection_id")
	require.NotContains(t, string(encoded), "connection_generation")
}
