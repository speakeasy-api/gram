package annotations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/corpus/annotations"
)

func createAnnotation(t *testing.T, ctx context.Context, ti *testInstance, filePath, content string) *annotations.Annotation {
	t.Helper()
	a, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, annotations.CreateParams{
		FilePath:   filePath,
		AuthorID:   "user-1",
		AuthorType: "human",
		Content:    content,
	})
	require.NoError(t, err)
	return a
}

func TestCreateAnnotation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	a, err := ti.svc.Create(ctx, ti.projectID, ti.orgID, annotations.CreateParams{
		FilePath:   "docs/overview.md",
		AuthorID:   "user-42",
		AuthorType: "human",
		Content:    "This section needs more detail.",
	})
	require.NoError(t, err)
	require.NotNil(t, a)
	require.Equal(t, "docs/overview.md", a.FilePath)
	require.Equal(t, "user-42", a.AuthorID)
	require.Equal(t, "human", a.AuthorType)
	require.Equal(t, "This section needs more detail.", a.Content)
	require.Equal(t, ti.projectID, a.ProjectID)
	require.False(t, a.Deleted)
}

func TestListAnnotations(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	createAnnotation(t, ctx, ti, "docs/guide.md", "note 1")
	createAnnotation(t, ctx, ti, "docs/guide.md", "note 2")
	createAnnotation(t, ctx, ti, "docs/guide.md", "note 3")

	list, err := ti.svc.List(ctx, ti.projectID, "docs/guide.md")
	require.NoError(t, err)
	require.Len(t, list, 3)

	// Assert ordered by created_at ASC
	require.Equal(t, "note 1", list[0].Content)
	require.Equal(t, "note 2", list[1].Content)
	require.Equal(t, "note 3", list[2].Content)
}

func TestDeleteAnnotation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	a := createAnnotation(t, ctx, ti, "docs/api.md", "remove me")

	deleted, err := ti.svc.Delete(ctx, ti.projectID, a.ID)
	require.NoError(t, err)
	require.True(t, deleted.Deleted)

	list, err := ti.svc.List(ctx, ti.projectID, "docs/api.md")
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestListAnnotations_FilterByFile(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	createAnnotation(t, ctx, ti, "docs/alpha.md", "alpha note 1")
	createAnnotation(t, ctx, ti, "docs/alpha.md", "alpha note 2")
	createAnnotation(t, ctx, ti, "docs/beta.md", "beta note 1")

	alphaList, err := ti.svc.List(ctx, ti.projectID, "docs/alpha.md")
	require.NoError(t, err)
	require.Len(t, alphaList, 2)
	require.Equal(t, "alpha note 1", alphaList[0].Content)
	require.Equal(t, "alpha note 2", alphaList[1].Content)

	betaList, err := ti.svc.List(ctx, ti.projectID, "docs/beta.md")
	require.NoError(t, err)
	require.Len(t, betaList, 1)
	require.Equal(t, "beta note 1", betaList[0].Content)
}
