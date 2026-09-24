package activities_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// titleCompletionStub answers every title request with a fixed title and
// records the prompt it was asked to title.
type titleCompletionStub struct {
	title  string
	calls  atomic.Int32
	prompt atomic.Pointer[string]
}

func (s *titleCompletionStub) GetCompletion(_ context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	s.calls.Add(1)
	if len(req.Messages) > 1 {
		prompt := openrouter.GetText(req.Messages[len(req.Messages)-1])
		s.prompt.Store(&prompt)
	}
	message := openrouter.CreateMessageAssistant(s.title)
	return &openrouter.CompletionResponse{
		StartTime: time.Now(), Message: &message, MessageID: "", Model: req.Model, Provider: "",
		Usage: openrouter.Usage{}, FinishReason: nil, ToolCalls: nil, Content: s.title, Annotations: nil,
	}, nil
}

func (s *titleCompletionStub) GetCompletionStream(context.Context, openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, fmt.Errorf("unexpected streaming completion")
}

func (s *titleCompletionStub) GetObjectCompletion(context.Context, openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, fmt.Errorf("unexpected object completion")
}

func (s *titleCompletionStub) CreateEmbeddings(context.Context, string, string, []string, ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, fmt.Errorf("unexpected embeddings request")
}

func (s *titleCompletionStub) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.PlatformKey(), nil
}

type titleTestChat struct {
	conn      *pgxpool.Pool
	repo      *chatrepo.Queries
	orgID     string
	projectID uuid.UUID
	chatID    uuid.UUID
}

func newTitleTestChat(t *testing.T, name, title string) titleTestChat {
	t.Helper()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Test Org", Slug: orgID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Test Project", Slug: "proj-" + uuid.NewString()[:8], OrganizationID: orgID,
	})
	require.NoError(t, err)

	cr := chatrepo.New(conn)
	chatID, err := cr.UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID: uuid.New(), ProjectID: project.ID, OrganizationID: orgID,
		Title: pgtype.Text{String: title, Valid: title != ""},
	})
	require.NoError(t, err)

	return titleTestChat{conn: conn, repo: cr, orgID: orgID, projectID: project.ID, chatID: chatID}
}

func (c titleTestChat) addMessage(t *testing.T, role, content string) {
	t.Helper()

	_, err := c.repo.CreateChatMessageReturningID(t.Context(), chatrepo.CreateChatMessageReturningIDParams{
		ChatID: c.chatID, ProjectID: uuid.NullUUID{UUID: c.projectID, Valid: true}, Role: role, Content: content,
		ToolCalls: nil, ToolCallID: pgtype.Text{},
	})
	require.NoError(t, err)
}

func (c titleTestChat) title(t *testing.T) string {
	t.Helper()

	row, err := c.repo.GetChat(t.Context(), chatrepo.GetChatParams{ID: c.chatID, ProjectID: c.projectID})
	require.NoError(t, err)
	return row.Title.String
}

func (c titleTestChat) run(t *testing.T, client openrouter.CompletionClient) {
	t.Helper()

	act := activities.NewGenerateChatTitle(testenv.NewLogger(t), c.conn, client)
	require.NoError(t, act.Do(t.Context(), activities.GenerateChatTitleArgs{
		ChatID: c.chatID.String(), OrgID: c.orgID, ProjectID: c.projectID.String(),
	}))
}

// The hook ingest path titles a new session after the message that opened it.
// That stand-in has to be replaced once there is a conversation to name,
// otherwise every captured session is permanently labelled with a truncated
// first prompt.
func TestGenerateChatTitle_ReplacesTitleDerivedFromFirstPrompt(t *testing.T) {
	t.Parallel()

	prompt := "we want to create a new cname record for our docs domain and i am not sure which one it should point at"
	tc := newTitleTestChat(t, "generatetitle_derived", chat.DerivedTitle(prompt))
	require.NotEqual(t, prompt, tc.title(t), "the seeded title should be a truncation of the prompt")

	tc.addMessage(t, "user", prompt)
	tc.addMessage(t, "assistant", "Point the CNAME at the docs load balancer and wait for propagation.")

	stub := &titleCompletionStub{title: "Docs Domain CNAME Setup"}
	tc.run(t, stub)

	require.Equal(t, "Docs Domain CNAME Setup", tc.title(t))
	require.Equal(t, int32(1), stub.calls.Load())
}

// Generation is also what rescues the fixed placeholders, including the one
// every archived Anthropic inference conversation is stored with.
func TestGenerateChatTitle_ReplacesInferencePlaceholder(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_placeholder", chat.DefaultInferenceChatTitle)
	tc.addMessage(t, "user", "Summarize the quarterly incident review for the platform team.")
	tc.addMessage(t, "assistant", "There were four incidents, three caused by expired certificates.")

	tc.run(t, &titleCompletionStub{title: "Quarterly Incident Review"})

	require.Equal(t, "Quarterly Incident Review", tc.title(t))
}

// A title a source deliberately chose — a Claude Tag channel label, an
// imported conversation name — is not a stand-in and must survive.
func TestGenerateChatTitle_LeavesSourceChosenTitle(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_sourcetitle", "Claude Tag in #dev-demo")
	tc.addMessage(t, "user", "can you take a look at the failing deploy")
	tc.addMessage(t, "assistant", "The deploy failed because the migration lock was still held.")

	stub := &titleCompletionStub{title: "Failing Deploy Investigation"}
	tc.run(t, stub)

	require.Equal(t, "Claude Tag in #dev-demo", tc.title(t))
	require.Zero(t, stub.calls.Load(), "an already-named chat must not spend a completion")
}

// Once a real title is on file a later turn must not re-title the chat, so the
// steady state of a long session is one completion, not one per turn.
func TestGenerateChatTitle_DoesNotRetitleAfterSuccess(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_once", chat.DefaultClaudeChatTitle)
	tc.addMessage(t, "user", "why is the risk scanner flagging every tool call")
	tc.addMessage(t, "assistant", "The policy matches on tool name prefixes, so it catches all of them.")

	stub := &titleCompletionStub{title: "Risk Scanner False Positives"}
	tc.run(t, stub)
	require.Equal(t, "Risk Scanner False Positives", tc.title(t))

	tc.addMessage(t, "user", "ok please narrow the policy")
	tc.run(t, stub)

	require.Equal(t, "Risk Scanner False Positives", tc.title(t))
	require.Equal(t, int32(1), stub.calls.Load())
}

// Agent transcripts carry multi-kilobyte turns. The prompt the titler sends
// must stay bounded, or naming a session costs as much as holding it.
func TestGenerateChatTitle_BoundsTheTranscriptItSends(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_bounded", chat.DefaultChatTitle)
	for range 12 {
		tc.addMessage(t, "user", stringOfRunes('a', 5000))
		tc.addMessage(t, "assistant", stringOfRunes('b', 5000))
	}

	stub := &titleCompletionStub{title: "Very Long Session"}
	tc.run(t, stub)

	prompt := stub.prompt.Load()
	require.NotNil(t, prompt)
	require.Less(t, len([]rune(*prompt)), 5000, "six turns of at most 600 runes each, not the raw transcript")
}

func stringOfRunes(r rune, count int) string {
	out := make([]rune, count)
	for i := range out {
		out[i] = r
	}
	return string(out)
}
