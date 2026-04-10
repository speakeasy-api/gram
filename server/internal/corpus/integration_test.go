package corpus_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/corpus/drafts"
	corpusgit "github.com/speakeasy-api/gram/server/internal/corpus/git"
	corpusgithttp "github.com/speakeasy-api/gram/server/internal/corpus/githttp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type e2eInstance struct {
	draftsSvc *drafts.Service
	gitRepo   *corpusgit.Repo
	projectID uuid.UUID
	orgID     string
}

func newE2EInstance(t *testing.T) (context.Context, *e2eInstance) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "corpus_e2e")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx := testenv.InitAuthContext(t, t.Context(), conn, sessionManager)

	authctx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authctx.ProjectID)

	repoDir := t.TempDir()
	gitRepo, err := corpusgit.InitBareRepo(repoDir)
	require.NoError(t, err)

	svc := drafts.NewService(conn, gitRepo, drafts.NewMutexWriteLock())

	return ctx, &e2eInstance{
		draftsSvc: svc,
		gitRepo:   gitRepo,
		projectID: *authctx.ProjectID,
		orgID:     authctx.ActiveOrganizationID,
	}
}

func (e *e2eInstance) createDraft(t *testing.T, ctx context.Context, path, op string, content *string) *drafts.Draft {
	t.Helper()
	d, err := e.draftsSvc.Create(ctx, e.projectID, e.orgID, drafts.CreateDraftParams{
		FilePath:        path,
		Title:           nil,
		OriginalContent: nil,
		AuthorUserID:    nil,
		AgentName:       nil,
		Content:         content,
		Operation:       op,
		Source:          nil,
		AuthorType:      nil,
		Labels:          nil,
	})
	require.NoError(t, err)
	return d
}

func TestCorpusE2E_PublishAndClone(t *testing.T) {
	t.Parallel()
	ctx, e := newE2EInstance(t)

	// 1. Create a draft
	content := "# Guide"
	created := e.createDraft(t, ctx, "docs/guide.md", drafts.OpCreate, &content)
	require.NotEqual(t, uuid.Nil, created.ID)

	// 2. Publish the draft
	commitSHA, err := e.draftsSvc.Publish(ctx, e.projectID, e.orgID, []uuid.UUID{created.ID})
	require.NoError(t, err)
	require.NotEmpty(t, commitSHA)

	// 3. Verify the file exists in the git repo via ReadBlob
	blob, err := e.gitRepo.ReadBlob("HEAD", "docs/guide.md")
	require.NoError(t, err)
	require.Equal(t, "# Guide", string(blob))

	// 4. Start an httptest server with the githttp handler
	handler := corpusgithttp.NewHandler(func(_ string) (string, error) {
		return e.gitRepo.Path(), nil
	})

	server := httptest.NewServer(http.StripPrefix("/project/corpus.git", handler))
	defer server.Close()

	// 5. Clone the repo via go-git PlainClone
	cloneDir := t.TempDir()
	clonedRepo, err := gogit.PlainClone(cloneDir, &gogit.CloneOptions{
		URL: server.URL + "/project/corpus.git",
	})
	require.NoError(t, err)

	// 6. Verify the cloned worktree contains "docs/guide.md" with correct content
	wt, err := clonedRepo.Worktree()
	require.NoError(t, err)

	f, err := wt.Filesystem.Open("docs/guide.md")
	require.NoError(t, err)
	clonedContent, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "# Guide", string(clonedContent))

	// 7. Push a new file ("pushed.md") back to the repo
	err = os.WriteFile(filepath.Join(cloneDir, "pushed.md"), []byte("pushed content"), 0o644)
	require.NoError(t, err)

	_, err = wt.Add("pushed.md")
	require.NoError(t, err)

	_, err = wt.Commit("add pushed.md", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	err = clonedRepo.Push(&gogit.PushOptions{
		RemoteName: "origin",
	})
	require.NoError(t, err)

	// 8. Verify "pushed.md" exists in the bare repo
	pushedBlob, err := e.gitRepo.ReadBlob("HEAD", "pushed.md")
	require.NoError(t, err)
	require.Equal(t, "pushed content", string(pushedBlob))
}

func TestCorpusE2E_DraftCRUDLifecycle(t *testing.T) {
	t.Parallel()
	ctx, e := newE2EInstance(t)

	// 1. Create draft
	content := "initial content"
	created := e.createDraft(t, ctx, "lifecycle.md", drafts.OpCreate, &content)
	require.NotEqual(t, uuid.Nil, created.ID)
	require.Equal(t, drafts.StatusOpen, created.Status)

	// 2. List drafts - expect 1
	all, err := e.draftsSvc.List(ctx, e.projectID, nil)
	require.NoError(t, err)
	require.Len(t, all, 1)

	// 3. Update draft content
	updated, err := e.draftsSvc.UpdateContent(ctx, e.projectID, created.ID, "updated content")
	require.NoError(t, err)
	require.True(t, updated.Content.Valid)
	require.Equal(t, "updated content", updated.Content.String)

	// 4. Get draft by ID - assert matches
	got, err := e.draftsSvc.Get(ctx, e.projectID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.True(t, got.Content.Valid)
	require.Equal(t, "updated content", got.Content.String)

	// 5. Delete draft - assert status "rejected"
	deleted, err := e.draftsSvc.Delete(ctx, e.projectID, created.ID)
	require.NoError(t, err)
	require.Equal(t, drafts.StatusRejected, deleted.Status)

	// 6. List drafts - expect 0 (deleted ones filtered by deleted IS FALSE)
	remaining, err := e.draftsSvc.List(ctx, e.projectID, nil)
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func TestCorpusE2E_Enrichments(t *testing.T) {
	t.Parallel()
	ctx, e := newE2EInstance(t)

	// 1. Create 2 drafts for different file paths
	c1 := "content one"
	c2 := "content two"
	e.createDraft(t, ctx, "product/overview.md", drafts.OpCreate, &c1)
	e.createDraft(t, ctx, "engineering/arch.md", drafts.OpCreate, &c2)

	// 2. Call Enrichments
	enrichments, err := e.draftsSvc.Enrichments(ctx, e.projectID)
	require.NoError(t, err)

	// 3. Assert each path has OpenDrafts=1
	require.Len(t, enrichments, 2)
	require.Equal(t, int64(1), enrichments["product/overview.md"].OpenDrafts)
	require.Equal(t, int64(1), enrichments["engineering/arch.md"].OpenDrafts)
}
