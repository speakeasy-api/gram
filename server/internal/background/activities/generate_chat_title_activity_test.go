package activities_test

import (
	"context"
	"fmt"
	"strings"
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
// records how often it was asked and the last prompt it saw.
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

// newTitleTestChat seeds an org, project and chat; an empty title stores NULL.
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

func (c titleTestChat) get(t *testing.T) chatrepo.GetChatRow {
	t.Helper()

	row, err := c.repo.GetChat(t.Context(), chatrepo.GetChatParams{ID: c.chatID, ProjectID: c.projectID})
	require.NoError(t, err)
	return row
}

func (c titleTestChat) run(t *testing.T, client openrouter.CompletionClient) {
	t.Helper()

	act := activities.NewGenerateChatTitle(testenv.NewLogger(t), c.conn, client)
	require.NoError(t, act.Do(t.Context(), activities.GenerateChatTitleArgs{
		ChatID: c.chatID.String(), OrgID: c.orgID, ProjectID: c.projectID.String(),
	}))
}

// The stand-in hook ingest seeds from the opening prompt must be replaced once
// there is a conversation to name.
func TestGenerateChatTitle_ReplacesTitleDerivedFromFirstPrompt(t *testing.T) {
	t.Parallel()

	prompt := "we want to create a new cname record for our docs domain and i am not sure which one it should point at"
	tc := newTitleTestChat(t, "generatetitle_derived", chat.DerivedTitle(prompt))
	require.NotEqual(t, prompt, tc.get(t).Title.String, "the seeded title should be a truncation of the prompt")

	tc.addMessage(t, "user", prompt)
	tc.addMessage(t, "assistant", "Point the CNAME at the docs load balancer and wait for propagation.")

	stub := &titleCompletionStub{title: "Docs Domain CNAME Setup"}
	tc.run(t, stub)

	require.Equal(t, "Docs Domain CNAME Setup", tc.get(t).Title.String)
	require.Equal(t, int32(1), stub.calls.Load())
}

func TestGenerateChatTitle_ReplacesInferencePlaceholder(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_placeholder", chat.DefaultInferenceChatTitle)
	tc.addMessage(t, "user", "Summarize the quarterly incident review for the platform team.")
	tc.addMessage(t, "assistant", "There were four incidents, three caused by expired certificates.")

	tc.run(t, &titleCompletionStub{title: "Quarterly Incident Review"})

	require.Equal(t, "Quarterly Incident Review", tc.get(t).Title.String)
}

// A title a source chose (a channel label, an imported conversation name) is
// not a stand-in and must survive without spending a completion.
func TestGenerateChatTitle_LeavesSourceChosenTitle(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_sourcetitle", "Claude Tag in #dev-demo")
	tc.addMessage(t, "user", "can you take a look at the failing deploy")
	tc.addMessage(t, "assistant", "The deploy failed because the migration lock was still held.")

	stub := &titleCompletionStub{title: "Failing Deploy Investigation"}
	tc.run(t, stub)

	require.Equal(t, "Claude Tag in #dev-demo", tc.get(t).Title.String)
	require.Zero(t, stub.calls.Load())
}

// Once a real title is on file a later turn must not re-title the chat.
func TestGenerateChatTitle_DoesNotRetitleAfterSuccess(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_once", chat.DefaultClaudeChatTitle)
	tc.addMessage(t, "user", "why is the risk scanner flagging every tool call")
	tc.addMessage(t, "assistant", "The policy matches on tool name prefixes, so it catches all of them.")

	stub := &titleCompletionStub{title: "Risk Scanner False Positives"}
	tc.run(t, stub)
	require.Equal(t, "Risk Scanner False Positives", tc.get(t).Title.String)

	tc.addMessage(t, "user", "ok please narrow the policy")
	tc.run(t, stub)

	require.Equal(t, "Risk Scanner False Positives", tc.get(t).Title.String)
	require.Equal(t, int32(1), stub.calls.Load())
}

// The prompt sent to the model is the last six turns, each cut to 600 runes
// including the ellipsis, in chronological order.
func TestGenerateChatTitle_BoundsTheTranscriptItSends(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_bounded", chat.DefaultChatTitle)
	for range 12 {
		tc.addMessage(t, "user", strings.Repeat("a", 5000))
		tc.addMessage(t, "assistant", strings.Repeat("b", 5000))
	}

	stub := &titleCompletionStub{title: "Very Long Session"}
	tc.run(t, stub)

	prompt := stub.prompt.Load()
	require.NotNil(t, prompt)
	turn := func(role, letter string) string { return role + ": " + strings.Repeat(letter, 599) + "…" }
	expected := strings.Join([]string{
		turn("user", "a"), turn("assistant", "b"),
		turn("user", "a"), turn("assistant", "b"),
		turn("user", "a"), turn("assistant", "b"),
	}, "\n")
	require.Equal(t, expected, *prompt)
}
