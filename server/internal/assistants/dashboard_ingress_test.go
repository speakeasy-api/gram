package assistants

import (
	"testing"

	"github.com/stretchr/testify/require"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func TestDashboardAdapterThreadContext(t *testing.T) {
	t.Parallel()

	got, err := dashboardAdapter{}.ThreadContext([]byte(`{"user_id":"user-1"}`))
	require.NoError(t, err)
	require.Contains(t, got, "AI Insights sidebar")
	require.Contains(t, got, "user-1")
}

func TestDashboardAdapterDecodeTurn(t *testing.T) {
	t.Parallel()

	got, err := dashboardAdapter{}.DecodeTurn(assistantThreadEventRecord{
		EventID:               "evt-1",
		NormalizedPayloadJSON: []byte(`{"text":"what are my top errors?","user_id":"user-1"}`),
	})
	require.NoError(t, err)
	require.Contains(t, got, "evt-1")
	require.Contains(t, got, "user-1")
	require.Contains(t, got, "what are my top errors?")
}

// Dashboard messages reach the assistant through the trigger dispatch path, so
// enabling the managed assistant must stand up a direct-ingress trigger instance
// pointing at it, and disabling must tear it down.
func TestManagedAssistantDashboardTriggerLifecycle(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_dashboard_trigger")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "dash-trigger")

	managed, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)

	target := triggerrepo.ListActiveTriggerInstancesByTargetParams{
		ProjectID:      projectID,
		DefinitionSlug: sourceKindDashboard,
		TargetKind:     bgtriggers.TargetKindAssistant,
		TargetRef:      managed.ID.String(),
	}

	instances, err := triggerrepo.New(conn).ListActiveTriggerInstancesByTarget(ctx, target)
	require.NoError(t, err)
	require.Len(t, instances, 1, "enable provisions one dashboard trigger instance")
	require.Equal(t, bgtriggers.KindDirect, mustDefinitionKind(t, instances[0].DefinitionSlug))

	require.NoError(t, core.DisableManagedAssistant(ctx, projectID))

	instances, err = triggerrepo.New(conn).ListActiveTriggerInstancesByTarget(ctx, target)
	require.NoError(t, err)
	require.Empty(t, instances, "disable tears the dashboard trigger instance down")
}

// Managed assistants provisioned before dashboard ingress existed have no
// dashboard trigger. The enable fast path is idempotent, so re-enabling heals a
// missing trigger rather than returning early without one.
func TestEnableManagedAssistantHealsMissingDashboardTrigger(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_dashboard_trigger_heal")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "dash-heal")

	managed, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)

	target := triggerrepo.ListActiveTriggerInstancesByTargetParams{
		ProjectID:      projectID,
		DefinitionSlug: sourceKindDashboard,
		TargetKind:     bgtriggers.TargetKindAssistant,
		TargetRef:      managed.ID.String(),
	}

	instances, err := triggerrepo.New(conn).ListActiveTriggerInstancesByTarget(ctx, target)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	// Simulate the pre-ingress state by tearing the trigger down out of band.
	_, err = triggerrepo.New(conn).DeleteTriggerInstance(ctx, triggerrepo.DeleteTriggerInstanceParams{
		ID:        instances[0].ID,
		ProjectID: projectID,
	})
	require.NoError(t, err)

	instances, err = triggerrepo.New(conn).ListActiveTriggerInstancesByTarget(ctx, target)
	require.NoError(t, err)
	require.Empty(t, instances)

	healed, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)
	require.Equal(t, managed.ID, healed.ID, "re-enable returns the existing managed assistant")

	instances, err = triggerrepo.New(conn).ListActiveTriggerInstancesByTarget(ctx, target)
	require.NoError(t, err)
	require.Len(t, instances, 1, "re-enable re-provisions the dashboard trigger")
}

func mustDefinitionKind(t *testing.T, slug string) bgtriggers.Kind {
	t.Helper()
	def, ok := bgtriggers.GetDefinition(slug)
	require.True(t, ok, "definition %q registered", slug)
	return def.Kind
}

// buildAssistantEventPayload maps a dispatched dashboard Task to the assistant
// thread event shape: dashboard source kind, a source ref carrying the user id,
// and the message as the normalized payload.
func TestBuildAssistantEventPayloadDashboard(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"text":"what are my top errors?","user_id":"user-1"}`)
	kind, sourceRef, normalized, source, err := buildAssistantEventPayload(bgtriggers.Task{
		DefinitionSlug: sourceKindDashboard,
		EventJSON:      payload,
		RawPayload:     payload,
	})
	require.NoError(t, err)
	require.Equal(t, sourceKindDashboard, kind)
	require.JSONEq(t, `{"user_id":"user-1"}`, string(sourceRef))
	require.Equal(t, payload, normalized)
	require.JSONEq(t, string(payload), string(source))
}
