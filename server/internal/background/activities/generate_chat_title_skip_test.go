package activities_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// completionClientSpy fails the test if the activity ever reaches out to the LLM.
type completionClientSpy struct{ t *testing.T }

func (s *completionClientSpy) GetCompletion(context.Context, openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	s.t.Fatalf("GetCompletion should not be called for a manually titled chat")
	return nil, nil
}

func (s *completionClientSpy) GetCompletionStream(context.Context, openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	s.t.Fatalf("GetCompletionStream should not be called for a manually titled chat")
	return nil, nil
}

func (s *completionClientSpy) GetObjectCompletion(context.Context, openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	s.t.Fatalf("GetObjectCompletion should not be called for a manually titled chat")
	return nil, nil
}

func (s *completionClientSpy) CreateEmbeddings(context.Context, string, string, []string, ...openrouter.EmbeddingOption) ([][]float32, error) {
	s.t.Fatalf("CreateEmbeddings should not be called for a manually titled chat")
	return nil, nil
}

// A chat whose title was set by a human must be skipped by auto title
// generation — its title stays put and no LLM call is made.
func TestGenerateChatTitle_SkipsManuallyTitledChat(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "generatetitle_skip")
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Test Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Test Project",
		Slug:           "proj-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	cr := chatrepo.New(conn)
	chatID, err := cr.UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             uuid.New(),
		ProjectID:      project.ID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	// Pin a manual title.
	require.NoError(t, cr.RenameChat(ctx, chatrepo.RenameChatParams{
		Title:            pgtype.Text{String: "Human Picked", Valid: true},
		TitleManuallySet: true,
		ID:               chatID,
		ProjectID:        project.ID,
	}))

	act := activities.NewGenerateChatTitle(logger, conn, &completionClientSpy{t: t})
	err = act.Do(ctx, activities.GenerateChatTitleArgs{
		ChatID:    chatID.String(),
		OrgID:     orgID,
		ProjectID: project.ID.String(),
	})
	require.NoError(t, err)

	chat, err := cr.GetChat(ctx, chatID)
	require.NoError(t, err)
	require.Equal(t, "Human Picked", chat.Title.String)
	require.True(t, chat.TitleManuallySet)
}
