package anthropicinference

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// recordingTitleGenerator records every chat the store asked to name.
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

// Scheduling stops once the conversation carries a real title, so an agent
// loop costs one workflow start rather than one per model call.
func TestProcessSchedulesTitleOnlyWhilePlaceholder(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	titles := &recordingTitleGenerator{}
	store.titles = titles
	service := &Service{logger: testenv.NewLogger(t), metrics: newMetrics(testenv.NewMeterProvider(t), testenv.NewLogger(t)), store: store, scanner: &recordingScanner{}}
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "EXAMPLE prompt"), textMessage("assistant", "EXAMPLE reply")}
	chatID := conversationID(config, frame)

	_, err := service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, []string{chatID.String()}, titles.scheduled())
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, chatrepo.New(db).UpdateChatTitle(t.Context(), chatrepo.UpdateChatTitleParams{
		ID: chatID, ProjectID: config.ProjectID, Title: pgtype.Text{String: "Example Conversation", Valid: true},
	}))
	frame.Messages = append(frame.Messages, textMessage("user", "EXAMPLE follow-up"))
	_, err = service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.Never(t, func() bool { return len(titles.scheduled()) > 1 }, 200*time.Millisecond, 10*time.Millisecond)
}
