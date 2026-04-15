-- name: CreateVote :one
INSERT INTO corpus_feedback (
    project_id,
    organization_id,
    file_path,
    user_id,
    direction
) VALUES (
    @project_id,
    @organization_id,
    @file_path,
    @user_id,
    @direction
)
RETURNING *;

-- name: GetLatestVoteForFileByUser :one
SELECT *
FROM corpus_feedback
WHERE project_id = @project_id
  AND file_path = @file_path
  AND user_id = @user_id
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: ListFeedbackByProject :many
SELECT
    file_path,
    COUNT(*) FILTER (WHERE direction = 'up') AS upvotes,
    COUNT(*) FILTER (WHERE direction = 'down') AS downvotes
FROM corpus_feedback
WHERE project_id = @project_id
GROUP BY file_path
ORDER BY file_path;

-- name: ListFeedbackForFile :many
SELECT
    file_path,
    COUNT(*) FILTER (WHERE direction = 'up') AS upvotes,
    COUNT(*) FILTER (WHERE direction = 'down') AS downvotes
FROM corpus_feedback
WHERE project_id = @project_id
  AND file_path = @file_path
GROUP BY file_path;

-- name: CreateComment :one
INSERT INTO corpus_feedback_comments (
    project_id,
    organization_id,
    file_path,
    author_id,
    author_type,
    content
) VALUES (
    @project_id,
    @organization_id,
    @file_path,
    @author_id,
    @author_type,
    @content
)
RETURNING *;

-- name: ListComments :many
SELECT *
FROM corpus_feedback_comments
WHERE project_id = @project_id
  AND file_path = @file_path
  AND deleted IS FALSE
ORDER BY created_at ASC;
