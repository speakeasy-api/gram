package assistants

import (
	"context"
	"testing"
	"time"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/stretchr/testify/require"
)

// Intentionally uses distinct protocol/core instances: cancellation cannot rely
// on sharing the RunTurn client's memory with the API process.
func TestKitInterruptMailboxCrossInstance(t *testing.T) {
	t.Parallel()
	core, db, key, _ := kitSessionFixture(t)
	_, err := core.StoreKitRuntimeSession(t.Context(), key, "owned-session")
	require.NoError(t, err)
	event, err := assistantrepo.New(db).ClaimNextPendingEvent(t.Context(), assistantrepo.ClaimNextPendingEventParams{
		ProjectID: key.ProjectID, ThreadID: key.ThreadID, PendingStatus: eventStatusPending, ProcessingStatus: eventStatusProcessing,
	})
	require.NoError(t, err)
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}
	req := runTurnRequest{EventID: event.ID, EventAttempt: int(event.Attempts), ThreadID: key.ThreadID}
	api := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Core: &ServiceCore{db: db}}}
	worker := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Core: core}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type result struct {
		interrupted bool
		err         error
	}
	done := make(chan result, 1)
	go func() { interrupted, err := api.InterruptTurn(ctx, r, key.ThreadID); done <- result{interrupted, err} }()
	require.Eventually(t, func() bool { pending, err := worker.kitInterruptPending(ctx, r, req); return err == nil && pending }, time.Second, 10*time.Millisecond)
	stale := req
	stale.EventAttempt++
	pending, err := worker.kitInterruptPending(ctx, r, stale)
	require.NoError(t, err)
	require.False(t, pending)
	require.NoError(t, worker.acknowledgeKitInterrupt(ctx, r, stale, "cancelled"))
	select {
	case <-done:
		t.Fatal("wrong attempt acknowledged request")
	default:
	}
	// Model the owner after cancellation terminal + authoritative settlement + flush.
	require.NoError(t, worker.acknowledgeKitInterrupt(ctx, r, req, "cancelled"))
	got := <-done
	require.NoError(t, got.err)
	require.True(t, got.interrupted)
}

func TestKitInterruptMailboxNoSession(t *testing.T) {
	t.Parallel()
	core, _, key, _ := kitSessionFixture(t)
	p := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Core: core}}
	got, err := p.InterruptTurn(t.Context(), assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}, key.ThreadID)
	require.NoError(t, err)
	require.False(t, got)
}
