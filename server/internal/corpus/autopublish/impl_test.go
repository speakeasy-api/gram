package autopublish_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/corpus/autopublish"
	"github.com/speakeasy-api/gram/server/internal/corpus/autopublish/repo"
	"github.com/speakeasy-api/gram/server/internal/corpus/drafts"
)

func TestGetConfig_Default(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cfg, err := ti.svc.GetConfig(ctx, ti.projectID)
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.Equal(t, int32(10), cfg.IntervalMinutes)
	require.Equal(t, int32(0), cfg.MinUpvotes)
	require.Nil(t, cfg.AuthorTypeFilter)
	require.Nil(t, cfg.LabelFilter)
	require.Equal(t, int32(0), cfg.MinAgeHours)
}

func TestSetConfig(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	agentFilter := "agent"
	input := autopublish.Config{
		Enabled:          true,
		IntervalMinutes:  30,
		MinUpvotes:       5,
		AuthorTypeFilter: &agentFilter,
		LabelFilter:      nil,
		MinAgeHours:      24,
	}

	result, err := ti.svc.SetConfig(ctx, ti.projectID, ti.orgID, input)
	require.NoError(t, err)
	require.True(t, result.Enabled)
	require.Equal(t, int32(30), result.IntervalMinutes)
	require.Equal(t, int32(5), result.MinUpvotes)
	require.NotNil(t, result.AuthorTypeFilter)
	require.Equal(t, "agent", *result.AuthorTypeFilter)
	require.Equal(t, int32(24), result.MinAgeHours)

	// Verify GetConfig returns the same
	got, err := ti.svc.GetConfig(ctx, ti.projectID)
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, int32(30), got.IntervalMinutes)
	require.Equal(t, int32(5), got.MinUpvotes)
	require.NotNil(t, got.AuthorTypeFilter)
	require.Equal(t, "agent", *got.AuthorTypeFilter)
	require.Equal(t, int32(24), got.MinAgeHours)
}

func TestQueryEligibleDrafts_UpvoteThreshold(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Create 3 drafts with different file paths
	c1, c2, c3 := "content1", "content2", "content3"
	d1 := createTestDraft(t, ctx, ti, "doc1.md", drafts.OpCreate, &c1)
	_ = createTestDraft(t, ctx, ti, "doc2.md", drafts.OpCreate, &c2)
	d3 := createTestDraft(t, ctx, ti, "doc3.md", drafts.OpCreate, &c3)

	// Add upvotes: doc1 gets 6, doc2 gets 2, doc3 gets 5
	for i := range 6 {
		_, err := ti.repo.InsertFeedback(ctx, repo.InsertFeedbackParams{
			ProjectID:      ti.projectID,
			OrganizationID: ti.orgID,
			FilePath:       "doc1.md",
			UserID:         uuid.New().String(),
			Direction:      "up",
		})
		require.NoError(t, err, "insert feedback doc1 user %d", i)
	}
	for i := range 2 {
		_, err := ti.repo.InsertFeedback(ctx, repo.InsertFeedbackParams{
			ProjectID:      ti.projectID,
			OrganizationID: ti.orgID,
			FilePath:       "doc2.md",
			UserID:         uuid.New().String(),
			Direction:      "up",
		})
		require.NoError(t, err, "insert feedback doc2 user %d", i)
	}
	for i := range 5 {
		_, err := ti.repo.InsertFeedback(ctx, repo.InsertFeedbackParams{
			ProjectID:      ti.projectID,
			OrganizationID: ti.orgID,
			FilePath:       "doc3.md",
			UserID:         uuid.New().String(),
			Direction:      "up",
		})
		require.NoError(t, err, "insert feedback doc3 user %d", i)
	}

	cfg := autopublish.Config{
		MinUpvotes:       5,
		AuthorTypeFilter: nil,
		MinAgeHours:      0,
	}
	eligible, err := ti.svc.QueryEligibleDrafts(ctx, ti.projectID, cfg)
	require.NoError(t, err)
	require.Len(t, eligible, 2)

	ids := make(map[uuid.UUID]bool)
	for _, d := range eligible {
		ids[d.ID] = true
	}
	require.True(t, ids[d1.ID], "doc1 with 6 upvotes should be eligible")
	require.True(t, ids[d3.ID], "doc3 with 5 upvotes should be eligible")
}

func TestQueryEligibleDrafts_AuthorTypeFilter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	agentType := "agent"
	humanType := "human"

	// Create agent draft
	c1 := "agent content"
	agentDraft, err := ti.draftsSvc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:        "agent-doc.md",
		Title:           nil,
		OriginalContent: nil,
		AuthorUserID:    nil,
		AgentName:       nil,
		Content:         &c1,
		Operation:       drafts.OpCreate,
		Source:          nil,
		AuthorType:      &agentType,
		Labels:          nil,
	})
	require.NoError(t, err)

	// Create human draft
	c2 := "human content"
	_, err = ti.draftsSvc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:        "human-doc.md",
		Title:           nil,
		OriginalContent: nil,
		AuthorUserID:    nil,
		AgentName:       nil,
		Content:         &c2,
		Operation:       drafts.OpCreate,
		Source:          nil,
		AuthorType:      &humanType,
		Labels:          nil,
	})
	require.NoError(t, err)

	cfg := autopublish.Config{
		MinUpvotes:       0,
		AuthorTypeFilter: &agentType,
		MinAgeHours:      0,
	}
	eligible, err := ti.svc.QueryEligibleDrafts(ctx, ti.projectID, cfg)
	require.NoError(t, err)
	require.Len(t, eligible, 1)
	require.Equal(t, agentDraft.ID, eligible[0].ID)
}

func TestQueryEligibleDrafts_AgeThreshold(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Create two drafts; then manually backdate one to 48 hours ago
	c1 := "old content"
	oldDraft := createTestDraft(t, ctx, ti, "old-doc.md", drafts.OpCreate, &c1)

	c2 := "new content"
	_ = createTestDraft(t, ctx, ti, "new-doc.md", drafts.OpCreate, &c2)

	// Backdate the old draft's created_at by updating it directly via SQL
	oldTime := time.Now().Add(-48 * time.Hour)
	_, err := ti.conn.Exec(ctx,
		"UPDATE corpus_drafts SET created_at = $1 WHERE id = $2 AND project_id = $3",
		oldTime, oldDraft.ID, ti.projectID)
	require.NoError(t, err)

	cfg := autopublish.Config{
		MinUpvotes:       0,
		AuthorTypeFilter: nil,
		MinAgeHours:      24,
	}
	eligible, err := ti.svc.QueryEligibleDrafts(ctx, ti.projectID, cfg)
	require.NoError(t, err)
	require.Len(t, eligible, 1)
	require.Equal(t, oldDraft.ID, eligible[0].ID)
}

func createTestDraft(t *testing.T, ctx context.Context, ti *testInstance, path, op string, content *string) *drafts.Draft {
	t.Helper()
	d, err := ti.draftsSvc.Create(ctx, ti.projectID, ti.orgID, drafts.CreateDraftParams{
		FilePath:        path,
		Title:           nil,
		OriginalContent: nil,
		AuthorUserID:    nil,
		AgentName:       nil,
		Content:         content,
		Operation:       op,
		Source:          nil,
		AuthorType:      conv.PtrEmpty("agent"),
		Labels:          nil,
	})
	require.NoError(t, err)
	return d
}
