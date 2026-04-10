package drafts_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/corpus/drafts"
	"github.com/speakeasy-api/gram/server/internal/corpus/drafts/repo"
)

func createDraft(t *testing.T, ctx context.Context, ti *testInstance, path, op string, content *string) *drafts.Draft {
	t.Helper()
	d, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   path,
		Content:    content,
		Operation:  op,
		Source:     nil,
		AuthorType: nil,
		Labels:     nil,
	})
	require.NoError(t, err)
	return d
}

func TestCreateDraft(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "# New Document"
	d, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "docs/new.md",
		Content:    &content,
		Operation:  drafts.OpCreate,
		Source:     new("api"),
		AuthorType: new("human"),
		Labels:     nil,
	})
	require.NoError(t, err)
	require.NotNil(t, d)
	require.Equal(t, "docs/new.md", d.FilePath)
	require.Equal(t, drafts.OpCreate, d.Operation)
	require.Equal(t, drafts.StatusOpen, d.Status)
	require.Equal(t, ti.projectID, d.ProjectID)
}

func TestCreateDraftValidation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:  "docs/new.md",
		Operation: "invalid",
		Content:   nil, Source: nil, AuthorType: nil, Labels: nil,
	})
	require.ErrorIs(t, err, drafts.ErrInvalidOperation)

	_, err = ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:  "",
		Operation: drafts.OpCreate,
		Content:   nil, Source: nil, AuthorType: nil, Labels: nil,
	})
	require.ErrorIs(t, err, drafts.ErrEmptyFilePath)
}

func TestListDrafts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	c1, c2, c3 := "a", "b", "c"
	createDraft(t, ctx, ti, "a.md", drafts.OpCreate, &c1)
	createDraft(t, ctx, ti, "b.md", drafts.OpCreate, &c2)
	createDraft(t, ctx, ti, "c.md", drafts.OpUpdate, &c3)

	all, err := ti.svc.List(ctx, ti.projectID, nil)
	require.NoError(t, err)
	require.Len(t, all, 3)

	openStatus := drafts.StatusOpen
	open, err := ti.svc.List(ctx, ti.projectID, &openStatus)
	require.NoError(t, err)
	require.Len(t, open, 3)
}

func TestGetDraft(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "hello"
	created := createDraft(t, ctx, ti, "readme.md", drafts.OpCreate, &content)

	got, err := ti.svc.Get(ctx, ti.projectID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "readme.md", got.FilePath)
}

func TestUpdateDraft(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "original"
	created := createDraft(t, ctx, ti, "doc.md", drafts.OpCreate, &content)

	updated, err := ti.svc.UpdateContent(ctx, ti.projectID, created.ID, "updated content")
	require.NoError(t, err)
	require.True(t, updated.Content.Valid)
	require.Equal(t, "updated content", updated.Content.String)
}

func TestDeleteDraft(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "to delete"
	created := createDraft(t, ctx, ti, "delete-me.md", drafts.OpCreate, &content)

	deleted, err := ti.svc.Delete(ctx, ti.projectID, created.ID)
	require.NoError(t, err)
	require.Equal(t, drafts.StatusRejected, deleted.Status)

	all, err := ti.svc.List(ctx, ti.projectID, nil)
	require.NoError(t, err)
	require.Len(t, all, 0)
}

func TestPublishDraft_CreatesFile(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "# New File"
	created := createDraft(t, ctx, ti, "overview.md", drafts.OpCreate, &content)

	commitSHA, err := ti.svc.Publish(ctx, ti.projectID, ti.orgID, []uuid.UUID{created.ID})
	require.NoError(t, err)
	require.NotEmpty(t, commitSHA)

	blob, err := ti.git.ReadBlob("HEAD", "overview.md")
	require.NoError(t, err)
	require.Equal(t, "# New File", string(blob))

	d, err := ti.svc.Get(ctx, ti.projectID, created.ID)
	require.NoError(t, err)
	require.Equal(t, drafts.StatusPublished, d.Status)
	require.True(t, d.CommitSha.Valid)
	require.Equal(t, commitSHA, d.CommitSha.String)
}

func TestPublishDraft_UpdatesFile(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ti.seedGitFile(t, "guide.md", []byte("original content"))

	content := "updated content"
	created := createDraft(t, ctx, ti, "guide.md", drafts.OpUpdate, &content)

	_, err := ti.svc.Publish(ctx, ti.projectID, ti.orgID, []uuid.UUID{created.ID})
	require.NoError(t, err)

	blob, err := ti.git.ReadBlob("HEAD", "guide.md")
	require.NoError(t, err)
	require.Equal(t, "updated content", string(blob))
}

func TestPublishDraft_DeletesFile(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ti.seedGitFile(t, "to-delete.md", []byte("will be deleted"))

	created := createDraft(t, ctx, ti, "to-delete.md", drafts.OpDelete, nil)

	_, err := ti.svc.Publish(ctx, ti.projectID, ti.orgID, []uuid.UUID{created.ID})
	require.NoError(t, err)

	_, err = ti.git.ReadBlob("HEAD", "to-delete.md")
	require.Error(t, err)
}

func TestPublishBatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	var ids []uuid.UUID
	for _, path := range []string{"a.md", "b.md", "c.md"} {
		content := "content for " + path
		d := createDraft(t, ctx, ti, path, drafts.OpCreate, &content)
		ids = append(ids, d.ID)
	}

	commitSHA, err := ti.svc.Publish(ctx, ti.projectID, ti.orgID, ids)
	require.NoError(t, err)
	require.NotEmpty(t, commitSHA)

	for _, id := range ids {
		d, err := ti.svc.Get(ctx, ti.projectID, id)
		require.NoError(t, err)
		require.Equal(t, drafts.StatusPublished, d.Status)
		require.True(t, d.CommitSha.Valid)
		require.Equal(t, commitSHA, d.CommitSha.String)
	}
}

func TestPublishDraft_OutboxRow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "outbox test"
	created := createDraft(t, ctx, ti, "outbox.md", drafts.OpCreate, &content)

	commitSHA, err := ti.svc.Publish(ctx, ti.projectID, ti.orgID, []uuid.UUID{created.ID})
	require.NoError(t, err)

	var outboxStatus string
	var outboxCommitSHA string
	err = ti.conn.QueryRow(ctx,
		"SELECT status, commit_sha FROM corpus_publish_events WHERE project_id = $1 AND commit_sha = $2",
		ti.projectID, commitSHA).Scan(&outboxStatus, &outboxCommitSHA)
	require.NoError(t, err)
	require.Equal(t, "pending", outboxStatus)
	require.Equal(t, commitSHA, outboxCommitSHA)
}

func TestEnrichments(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	c1, c2 := "content1", "content2"
	createDraft(t, ctx, ti, "product/overview.md", drafts.OpCreate, &c1)
	createDraft(t, ctx, ti, "engineering/arch.md", drafts.OpCreate, &c2)

	enrichments, err := ti.svc.Enrichments(ctx, ti.projectID)
	require.NoError(t, err)
	require.Len(t, enrichments, 2)
	require.Equal(t, int64(1), enrichments["product/overview.md"].OpenDrafts)
	require.Equal(t, int64(1), enrichments["engineering/arch.md"].OpenDrafts)
}

func TestEnrichments_NoDrafts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	enrichments, err := ti.svc.Enrichments(ctx, ti.projectID)
	require.NoError(t, err)
	require.Empty(t, enrichments)
}

var _ drafts.Draft = repo.CorpusDraft{}
