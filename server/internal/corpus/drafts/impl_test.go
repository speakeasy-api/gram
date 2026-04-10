package drafts_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/corpus/drafts"
	"github.com/speakeasy-api/gram/server/internal/corpus/drafts/repo"
)

func TestCreateDraft(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "# New Document"
	d, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "docs/new.md",
		Content:    &content,
		Operation:  "create",
		Source:     new("api"),
		AuthorType: new("human"),
		Labels:     nil,
	})
	require.NoError(t, err)
	require.NotNil(t, d)
	require.Equal(t, "docs/new.md", d.FilePath)
	require.Equal(t, "create", d.Operation)
	require.Equal(t, "open", d.Status)
	require.Equal(t, ti.projectID, d.ProjectID)
}

func TestCreateDraftValidation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Invalid operation
	_, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "docs/new.md",
		Operation:  "invalid",
		Content:    nil,
		Source:     nil,
		AuthorType: nil,
		Labels:     nil,
	})
	require.ErrorIs(t, err, drafts.ErrInvalidOperation)

	// Empty file path
	_, err = ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "",
		Operation:  "create",
		Content:    nil,
		Source:     nil,
		AuthorType: nil,
		Labels:     nil,
	})
	require.ErrorIs(t, err, drafts.ErrEmptyFilePath)
}

func TestListDrafts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	for i, path := range []string{"a.md", "b.md", "c.md"} {
		content := "content " + path
		op := "create"
		if i == 2 {
			op = "update"
		}
		_, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
			FilePath:   path,
			Content:    &content,
			Operation:  op,
			Source:     nil,
			AuthorType: nil,
			Labels:     nil,
		})
		require.NoError(t, err)
	}

	all, err := ti.svc.List(ctx, ti.projectID, nil)
	require.NoError(t, err)
	require.Len(t, all, 3)

	// Filter by status
	openStatus := "open"
	open, err := ti.svc.List(ctx, ti.projectID, &openStatus)
	require.NoError(t, err)
	require.Len(t, open, 3)
}

func TestGetDraft(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "hello"
	created, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "readme.md",
		Content:    &content,
		Operation:  "create",
		Source:     nil,
		AuthorType: nil,
		Labels:     nil,
	})
	require.NoError(t, err)

	got, err := ti.svc.Get(ctx, ti.projectID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "readme.md", got.FilePath)
}

func TestUpdateDraft(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "original"
	created, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "doc.md",
		Content:    &content,
		Operation:  "create",
		Source:     nil,
		AuthorType: nil,
		Labels:     nil,
	})
	require.NoError(t, err)

	updated, err := ti.svc.UpdateContent(ctx, ti.projectID, created.ID, "updated content")
	require.NoError(t, err)
	require.True(t, updated.Content.Valid)
	require.Equal(t, "updated content", updated.Content.String)
}

func TestDeleteDraft(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "to delete"
	created, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "delete-me.md",
		Content:    &content,
		Operation:  "create",
		Source:     nil,
		AuthorType: nil,
		Labels:     nil,
	})
	require.NoError(t, err)

	deleted, err := ti.svc.Delete(ctx, ti.projectID, created.ID)
	require.NoError(t, err)
	require.Equal(t, "rejected", deleted.Status)

	// Should not appear in list
	all, err := ti.svc.List(ctx, ti.projectID, nil)
	require.NoError(t, err)
	require.Len(t, all, 0)
}

func TestPublishDraft_CreatesFile(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "# New File"
	created, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "product/overview.md",
		Content:    &content,
		Operation:  "create",
		Source:     nil,
		AuthorType: nil,
		Labels:     nil,
	})
	require.NoError(t, err)

	commitSHA, err := ti.svc.Publish(ctx, ti.projectID, ti.orgID, []uuid.UUID{created.ID})
	require.NoError(t, err)
	require.NotEmpty(t, commitSHA)

	// Verify file in git
	blob, err := ti.svc.ReadGitBlob("HEAD", "product/overview.md")
	require.NoError(t, err)
	require.Equal(t, "# New File", string(blob))

	// Verify draft status updated
	d, err := ti.svc.Get(ctx, ti.projectID, created.ID)
	require.NoError(t, err)
	require.Equal(t, "published", d.Status)
	require.True(t, d.CommitSha.Valid)
	require.Equal(t, commitSHA, d.CommitSha.String)
}

func TestPublishDraft_UpdatesFile(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Seed git repo with existing file
	_, err := ti.svc.SeedGitFile("docs/guide.md", []byte("original content"))
	require.NoError(t, err)

	content := "updated content"
	created, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "docs/guide.md",
		Content:    &content,
		Operation:  "update",
		Source:     nil,
		AuthorType: nil,
		Labels:     nil,
	})
	require.NoError(t, err)

	_, err = ti.svc.Publish(ctx, ti.projectID, ti.orgID, []uuid.UUID{created.ID})
	require.NoError(t, err)

	blob, err := ti.svc.ReadGitBlob("HEAD", "docs/guide.md")
	require.NoError(t, err)
	require.Equal(t, "updated content", string(blob))
}

func TestPublishDraft_DeletesFile(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Seed git repo with file
	_, err := ti.svc.SeedGitFile("to-delete.md", []byte("will be deleted"))
	require.NoError(t, err)

	created, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "to-delete.md",
		Operation:  "delete",
		Content:    nil,
		Source:     nil,
		AuthorType: nil,
		Labels:     nil,
	})
	require.NoError(t, err)

	_, err = ti.svc.Publish(ctx, ti.projectID, ti.orgID, []uuid.UUID{created.ID})
	require.NoError(t, err)

	// File should be gone
	_, err = ti.svc.ReadGitBlob("HEAD", "to-delete.md")
	require.Error(t, err)
}

func TestPublishBatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	var ids []uuid.UUID
	for _, path := range []string{"a.md", "b.md", "c.md"} {
		content := "content for " + path
		d, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
			FilePath:   path,
			Content:    &content,
			Operation:  "create",
			Source:     nil,
			AuthorType: nil,
			Labels:     nil,
		})
		require.NoError(t, err)
		ids = append(ids, d.ID)
	}

	commitSHA, err := ti.svc.Publish(ctx, ti.projectID, ti.orgID, ids)
	require.NoError(t, err)
	require.NotEmpty(t, commitSHA)

	// All drafts should have same commit SHA
	for _, id := range ids {
		d, err := ti.svc.Get(ctx, ti.projectID, id)
		require.NoError(t, err)
		require.Equal(t, "published", d.Status)
		require.True(t, d.CommitSha.Valid)
		require.Equal(t, commitSHA, d.CommitSha.String)
	}
}

func TestPublishDraft_OutboxRow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	content := "outbox test"
	created, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:   "outbox.md",
		Content:    &content,
		Operation:  "create",
		Source:     nil,
		AuthorType: nil,
		Labels:     nil,
	})
	require.NoError(t, err)

	commitSHA, err := ti.svc.Publish(ctx, ti.projectID, ti.orgID, []uuid.UUID{created.ID})
	require.NoError(t, err)

	// Check outbox row exists
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

	c1 := "content1"
	c2 := "content2"
	_, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:  "product/overview.md",
		Content:   &c1,
		Operation: "create",
		Source:    nil, AuthorType: nil, Labels: nil,
	})
	require.NoError(t, err)
	_, err = ti.svc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:  "engineering/arch.md",
		Content:   &c2,
		Operation: "create",
		Source:    nil, AuthorType: nil, Labels: nil,
	})
	require.NoError(t, err)

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
