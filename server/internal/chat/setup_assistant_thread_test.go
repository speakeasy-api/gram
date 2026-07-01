package chat_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// seedAssistant creates a minimal active assistant for the test project.
func seedAssistant(t *testing.T, ctx context.Context, ti *chatTestInstance) uuid.UUID {
	t.Helper()
	a, err := assistantsrepo.New(ti.conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		Name:           "Setup Assistant " + uuid.NewString()[:8],
		Model:          "anthropic/claude-opus-4.8",
		Instructions:   "be helpful",
		WarmTtlSeconds: 300,
		MaxConcurrency: 1,
		Status:         "active",
	})
	require.NoError(t, err)
	return a.ID
}

// TestUpsertSetupAssistantThread_MakesChatListable verifies the onboarding
// feature: a client-side setup chat (owned by the dashboard user) that gets
// linked to its assistant via UpsertSetupAssistantThread becomes listable
// through chat.list?assistant_id=, exactly like a runtime assistant thread.
// This is what lets the assistants setup page resurface prior onboarding
// threads instead of silently losing them.
func TestUpsertSetupAssistantThread_MakesChatListable(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	assistantID := seedAssistant(t, ctx, ti)

	// A user-owned setup chat that is NOT yet linked to the assistant must not
	// appear when filtering by that assistant.
	setupChatID := seedChat(t, ctx, ti, authCtx.UserID, "", "Setup Chat")

	assistantIDStr := assistantID.String()
	payload := defaultPayload()
	payload.AssistantID = &assistantIDStr

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 0, result.Total, "unlinked setup chat must not be listed under the assistant")

	// Link it the way the completion handler does: source_kind=setup,
	// correlation_id=chat id.
	r := repo.New(ti.conn)
	_, err = r.UpsertSetupAssistantThread(ctx, repo.UpsertSetupAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     ti.projectID,
		CorrelationID: setupChatID.String(),
		ChatID:        setupChatID,
	})
	require.NoError(t, err)

	// Now it is listable under the assistant filter, scoped to the owning user.
	result, err = ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Chats, 1)
	require.Equal(t, setupChatID.String(), result.Chats[0].ID)
	require.Equal(t, authCtx.UserID, conv.PtrValOr(result.Chats[0].UserID, ""))
}

// TestUpsertSetupAssistantThread_Idempotent verifies the handler can fire the
// upsert on every completion for the chat without creating duplicate threads
// or failing (ON CONFLICT on project_id/assistant_id/correlation_id).
func TestUpsertSetupAssistantThread_Idempotent(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	assistantID := seedAssistant(t, ctx, ti)
	setupChatID := seedChat(t, ctx, ti, authCtx.UserID, "", "Setup Chat")

	r := repo.New(ti.conn)
	params := repo.UpsertSetupAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     ti.projectID,
		CorrelationID: setupChatID.String(),
		ChatID:        setupChatID,
	}
	id1, err := r.UpsertSetupAssistantThread(ctx, params)
	require.NoError(t, err)
	id2, err := r.UpsertSetupAssistantThread(ctx, params)
	require.NoError(t, err)
	require.Equal(t, id1, id2, "repeated upserts must resolve to the same thread row")

	// Still exactly one listed chat.
	assistantIDStr := assistantID.String()
	payload := defaultPayload()
	payload.AssistantID = &assistantIDStr
	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
}

// TestSetupThread_NotCountedAsActiveRuntimeThread is the concurrency-inertness
// correctness check: a source_kind=setup thread carries no runtime events and
// must never consume max_concurrency / warm-pool headroom. Even though the
// setup thread was just linked (recent last_event_at, no pending events), it
// must be excluded from CountActiveAssistantThreads, while a real runtime
// thread linked the same way IS counted.
func TestSetupThread_NotCountedAsActiveRuntimeThread(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	assistantID := seedAssistant(t, ctx, ti)

	// A freshly linked setup thread (recent last_event_at, no pending events).
	setupChatID := seedChat(t, ctx, ti, authCtx.UserID, "", "Setup Chat")
	cr := repo.New(ti.conn)
	_, err := cr.UpsertSetupAssistantThread(ctx, repo.UpsertSetupAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     ti.projectID,
		CorrelationID: setupChatID.String(),
		ChatID:        setupChatID,
	})
	require.NoError(t, err)

	ar := assistantsrepo.New(ti.conn)
	countParams := assistantsrepo.CountActiveAssistantThreadsParams{
		ProjectID:        ti.projectID,
		AssistantID:      assistantID,
		WarmupSourceKind: "warmup",
		SetupSourceKind:  "setup",
		ActiveSince:      conv.ToPGTimestamptz(time.Now().UTC().Add(-5 * time.Minute)),
		PendingStatus:    "pending",
	}
	active, err := ar.CountActiveAssistantThreads(ctx, countParams)
	require.NoError(t, err)
	require.Equal(t, int64(0), active, "setup thread must not count toward active runtime concurrency")

	// A real runtime thread (source_kind other than setup/warmup) linked the
	// same way IS counted — proves the exclusion is specific to setup, not a
	// query that counts nothing.
	runtimeChatID := seedChat(t, ctx, ti, authCtx.UserID, "", "Runtime Chat")
	_, err = ar.UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     ti.projectID,
		CorrelationID: "runtime-" + uuid.NewString()[:8],
		ChatID:        runtimeChatID,
		SourceKind:    "cron",
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)

	active, err = ar.CountActiveAssistantThreads(ctx, countParams)
	require.NoError(t, err)
	require.Equal(t, int64(1), active, "a real runtime thread must still be counted")
}
