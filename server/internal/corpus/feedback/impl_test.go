package feedback_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/corpus/feedback"
)

func TestVote_Up(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	vote, err := ti.svc.Vote(ctx, ti.projectID, ti.orgID, feedback.VoteParams{
		FilePath:  "docs/overview.md",
		UserID:    ti.userID,
		Direction: feedback.DirectionUp,
	})
	require.NoError(t, err)
	require.NotNil(t, vote)
	require.Equal(t, "docs/overview.md", vote.FilePath)
	require.Equal(t, ti.userID, vote.UserID)
	require.Equal(t, feedback.DirectionUp, vote.Direction)
	require.Equal(t, ti.projectID, vote.ProjectID)
}

func TestVote_RepeatedDirectionAccumulates(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.svc.Vote(ctx, ti.projectID, ti.orgID, feedback.VoteParams{
		FilePath:  "docs/guide.md",
		UserID:    ti.userID,
		Direction: feedback.DirectionUp,
	})
	require.NoError(t, err)

	vote, err := ti.svc.Vote(ctx, ti.projectID, ti.orgID, feedback.VoteParams{
		FilePath:  "docs/guide.md",
		UserID:    ti.userID,
		Direction: feedback.DirectionUp,
	})
	require.NoError(t, err)
	require.NotNil(t, vote)
	require.Equal(t, feedback.DirectionUp, vote.Direction)

	list, err := ti.svc.ListFeedback(ctx, ti.projectID, nil)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, int64(2), list[0].Upvotes)
	require.Equal(t, int64(0), list[0].Downvotes)

	filePath := "docs/guide.md"
	fileList, err := ti.svc.ListFeedback(ctx, ti.projectID, &filePath)
	require.NoError(t, err)
	require.Len(t, fileList, 1)
	require.Equal(t, int64(2), fileList[0].Upvotes)
	require.Equal(t, int64(0), fileList[0].Downvotes)
}

func TestVote_OppositeDirectionAlsoAccumulates(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.svc.Vote(ctx, ti.projectID, ti.orgID, feedback.VoteParams{
		FilePath:  "docs/api.md",
		UserID:    ti.userID,
		Direction: feedback.DirectionUp,
	})
	require.NoError(t, err)

	vote, err := ti.svc.Vote(ctx, ti.projectID, ti.orgID, feedback.VoteParams{
		FilePath:  "docs/api.md",
		UserID:    ti.userID,
		Direction: feedback.DirectionDown,
	})
	require.NoError(t, err)
	require.NotNil(t, vote)
	require.Equal(t, feedback.DirectionDown, vote.Direction)

	filePath := "docs/api.md"
	fileList, err := ti.svc.ListFeedback(ctx, ti.projectID, &filePath)
	require.NoError(t, err)
	require.Len(t, fileList, 1)
	require.Equal(t, int64(1), fileList[0].Upvotes)
	require.Equal(t, int64(1), fileList[0].Downvotes)
}

func TestLatestVoteDirection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	filePath := "docs/reference.md"

	direction, err := ti.svc.LatestVoteDirection(ctx, ti.projectID, filePath, ti.userID)
	require.NoError(t, err)
	require.Nil(t, direction)

	_, err = ti.svc.Vote(ctx, ti.projectID, ti.orgID, feedback.VoteParams{
		FilePath:  filePath,
		UserID:    ti.userID,
		Direction: feedback.DirectionUp,
	})
	require.NoError(t, err)

	_, err = ti.svc.Vote(ctx, ti.projectID, ti.orgID, feedback.VoteParams{
		FilePath:  filePath,
		UserID:    ti.userID,
		Direction: feedback.DirectionDown,
	})
	require.NoError(t, err)

	direction, err = ti.svc.LatestVoteDirection(ctx, ti.projectID, filePath, ti.userID)
	require.NoError(t, err)
	require.NotNil(t, direction)
	require.Equal(t, feedback.DirectionDown, *direction)
}

func TestListFeedback(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Vote on 3 files
	for _, path := range []string{"a.md", "b.md", "c.md"} {
		_, err := ti.svc.Vote(ctx, ti.projectID, ti.orgID, feedback.VoteParams{
			FilePath:  path,
			UserID:    ti.userID,
			Direction: feedback.DirectionUp,
		})
		require.NoError(t, err)
	}

	// List all feedback for project
	list, err := ti.svc.ListFeedback(ctx, ti.projectID, nil)
	require.NoError(t, err)
	require.Len(t, list, 3)

	// Each should have 1 upvote, 0 downvotes
	byPath := make(map[string]feedback.FeedbackSummary)
	for _, item := range list {
		byPath[item.FilePath] = item
	}
	for _, path := range []string{"a.md", "b.md", "c.md"} {
		summary, ok := byPath[path]
		require.True(t, ok, "expected summary for %s", path)
		require.Equal(t, int64(1), summary.Upvotes)
		require.Equal(t, int64(0), summary.Downvotes)
	}

	// List feedback for specific file
	filePath := "a.md"
	fileList, err := ti.svc.ListFeedback(ctx, ti.projectID, &filePath)
	require.NoError(t, err)
	require.Len(t, fileList, 1)
	require.Equal(t, "a.md", fileList[0].FilePath)
}

func TestAddComment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	comment, err := ti.svc.AddComment(ctx, ti.projectID, ti.orgID, feedback.AddCommentParams{
		FilePath:   "docs/overview.md",
		AuthorID:   ti.userID,
		AuthorType: "human",
		Content:    "This page is really helpful!",
	})
	require.NoError(t, err)
	require.NotNil(t, comment)
	require.Equal(t, "docs/overview.md", comment.FilePath)
	require.Equal(t, ti.userID, comment.AuthorID)
	require.Equal(t, "human", comment.AuthorType)
	require.Equal(t, "This page is really helpful!", comment.Content)
	require.Equal(t, ti.projectID, comment.ProjectID)
}

func TestListComments(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Add 3 comments on same file
	for _, content := range []string{"first comment", "second comment", "third comment"} {
		_, err := ti.svc.AddComment(ctx, ti.projectID, ti.orgID, feedback.AddCommentParams{
			FilePath:   "docs/guide.md",
			AuthorID:   ti.userID,
			AuthorType: "human",
			Content:    content,
		})
		require.NoError(t, err)
	}

	// List comments for file, assert ordered by created_at
	comments, err := ti.svc.ListComments(ctx, ti.projectID, "docs/guide.md")
	require.NoError(t, err)
	require.Len(t, comments, 3)
	require.Equal(t, "first comment", comments[0].Content)
	require.Equal(t, "second comment", comments[1].Content)
	require.Equal(t, "third comment", comments[2].Content)

	// Verify ordering by created_at ascending
	for i := 1; i < len(comments); i++ {
		require.False(t, comments[i].CreatedAt.Time.Before(comments[i-1].CreatedAt.Time),
			"comments should be ordered by created_at ascending")
	}
}
