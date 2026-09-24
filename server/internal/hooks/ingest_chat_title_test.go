package hooks

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// recordingTitleGenerator records the chats unified ingest asked to name.
type recordingTitleGenerator struct {
	mu      sync.Mutex
	chatIDs []string
}

func (g *recordingTitleGenerator) ScheduleChatTitleGeneration(_ context.Context, chatID, _, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.chatIDs = append(g.chatIDs, chatID)
	return nil
}

func (g *recordingTitleGenerator) scheduled() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.chatIDs...)
}

// Unified ingest seeds a session with a stand-in title derived from its
// opening prompt. Without scheduling generation the session wears that
// truncated prompt forever, which is what every hook-captured session used to
// do — the per-platform endpoints schedule, this path did not.
func TestIngest_SchedulesChatTitleGenerationForConversationTurns(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	titles := &recordingTitleGenerator{}
	ti.service.chatTitleGenerator = titles

	sessionID := "title-ingest-" + uuid.NewString()
	prompt := "combine the domain verification task into the main setup task, we no longer need it standing on its own"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	_, err := ti.service.IngestAuthenticated(ctx, authCtx, payload)
	require.NoError(t, err)

	reply := "Merged the two tasks; the verification steps are now step three of setup."
	response := canonicalIngestPayload("claude", "assistant.responded", sessionID)
	response.Data = &gen.HookIngestData{Message: &gen.HookMessageData{Text: &reply}}
	_, err = ti.service.IngestAuthenticated(ctx, authCtx, response)
	require.NoError(t, err)

	chatID := sessionIDToUUID(sessionID).String()
	require.Equal(t, []string{chatID, chatID}, titles.scheduled())

	// The stand-in the session is stored with is exactly what the title
	// generator re-derives to decide it may replace it.
	stored, err := chatRepo.New(ti.conn).GetChat(ctx, chatRepo.GetChatParams{
		ID: sessionIDToUUID(sessionID), ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, chat.DerivedTitle(prompt), stored.Title.String)
	require.True(t, chat.IsDerivedTitle(stored.Title.String, prompt))
}

// Tool traffic says how the agent worked, not what it was asked to do, and a
// busy session emits far more of it than conversation. Naming is only worth
// scheduling on the turns that carry the topic.
func TestIngest_DoesNotScheduleChatTitleGenerationForToolTraffic(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	titles := &recordingTitleGenerator{}
	ti.service.chatTitleGenerator = titles

	sessionID := "title-tools-" + uuid.NewString()
	toolName := "Bash"
	request := canonicalIngestPayload("claude", "tool.requested", sessionID)
	request.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{Name: &toolName}}
	_, err := ti.service.IngestAuthenticated(ctx, authCtx, request)
	require.NoError(t, err)

	completion := canonicalIngestPayload("claude", "tool.completed", sessionID)
	completion.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{Name: &toolName, Output: "exit status 0"}}
	_, err = ti.service.IngestAuthenticated(ctx, authCtx, completion)
	require.NoError(t, err)

	messages, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID: sessionIDToUUID(sessionID), ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, messages, 2, "both tool rows landed, so nothing was scheduled because of the event type")
	require.Empty(t, titles.scheduled())
}

// A Claude Tag session is titled after the channel it belongs to, refreshed on
// every wake. A generated name would be overwritten on the next message, so
// the completion is never worth spending.
func TestIngest_DoesNotScheduleChatTitleGenerationForClaudeTag(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	titles := &recordingTitleGenerator{}
	ti.service.chatTitleGenerator = titles

	sessionID := "title-tag-" + uuid.NewString()
	prompt := `<wake reason="channel-activity"><channel id="DEMO_CHANNEL"><message from="human" trigger="true">&lt;@DEMO_BOT|Claude&gt; can you look into this</message></channel></wake>`
	payload := canonicalIngestPayload("claude-code", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	ti.service.recordCanonicalHook(ctx, payload, authCtx, canonicalActor{UserID: "", Email: ""}, time.Now(), "")

	require.Empty(t, titles.scheduled())

	stored, err := chatRepo.New(ti.conn).GetChat(ctx, chatRepo.GetChatParams{
		ID: sessionIDToUUID(sessionID), ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, "Claude Tag in #DEMO_CHANNEL", stored.Title.String)
}
