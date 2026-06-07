package assistants

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func TestDashboardAdapterThreadContext(t *testing.T) {
	t.Parallel()

	got, err := dashboardAdapter{}.ThreadContext([]byte(`{"user_id":"user-1"}`))
	require.NoError(t, err)
	require.Contains(t, got, "Gram dashboard")
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
	// No event timestamp → no Timestamp line (zero value is omitted).
	require.NotContains(t, got, "Timestamp:")
}

func TestDashboardAdapterDecodeTurnStampsTimestamp(t *testing.T) {
	t.Parallel()

	event := assistantThreadEventRecord{
		EventID:               "evt-1",
		CreatedAt:             time.Date(2026, time.June, 6, 23, 36, 22, 0, time.UTC),
		NormalizedPayloadJSON: []byte(`{"text":"what are my top errors?","user_id":"user-1"}`),
	}

	got, err := dashboardAdapter{}.DecodeTurn(event)
	require.NoError(t, err)
	require.Contains(t, got, "Timestamp: 2026-06-06T23:36:22Z")

	// Re-decoding the same event must be byte-identical: the capture matcher
	// compares stored content against replayed content, so a non-deterministic
	// decode would open a spurious new generation on every retry/replay.
	again, err := dashboardAdapter{}.DecodeTurn(event)
	require.NoError(t, err)
	require.Equal(t, got, again)
}

// A non-UTC event clock is normalized to UTC in the stamp.
func TestDashboardAdapterDecodeTurnStampsTimestampInUTC(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("west", -3*60*60)
	event := assistantThreadEventRecord{
		EventID:               "evt-1",
		CreatedAt:             time.Date(2026, time.June, 6, 23, 36, 22, 0, loc),
		NormalizedPayloadJSON: []byte(`{"text":"hi"}`),
	}

	got, err := dashboardAdapter{}.DecodeTurn(event)
	require.NoError(t, err)
	require.Contains(t, got, "Timestamp: 2026-06-07T02:36:22Z")
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

func TestIsWarmDashboardEvent(t *testing.T) {
	t.Parallel()

	require.True(t, isWarmDashboardEvent(sourceKindDashboard, []byte(`{"warm":true}`)))
	require.False(t, isWarmDashboardEvent(sourceKindDashboard, []byte(`{"text":"hi","user_id":"u1"}`)), "a real turn is not warm")
	require.False(t, isWarmDashboardEvent("slack", []byte(`{"warm":true}`)), "warm only applies to the dashboard source")
	require.False(t, isWarmDashboardEvent(sourceKindDashboard, []byte(`not json`)), "malformed payload is not warm")
}

// A warm event must keep its warm marker all the way through
// buildAssistantEventPayload into the normalized payload — otherwise
// processEventTurn would run a real (empty) turn instead of skipping it. This is
// the round-trip that the earlier `hidden` flag lost.
func TestBuildAssistantEventPayloadDashboardWarm(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"text":"","warm":true}`)
	kind, _, normalized, _, err := buildAssistantEventPayload(bgtriggers.Task{
		DefinitionSlug: sourceKindDashboard,
		EventJSON:      payload,
		RawPayload:     payload,
	})
	require.NoError(t, err)
	require.Equal(t, sourceKindDashboard, kind)
	require.True(t, isWarmDashboardEvent(kind, normalized), "warm survives into the normalized payload")
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
