-- name: CreateAnnotation :one
INSERT INTO corpus_annotations (
    project_id,
    organization_id,
    file_path,
    author_id,
    author_type,
    content,
    line_start,
    line_end
) VALUES (
    @project_id,
    @organization_id,
    @file_path,
    @author_id,
    @author_type,
    @content,
    @line_start,
    @line_end
)
RETURNING *;

-- name: ListAnnotationsByFilePath :many
SELECT *
FROM corpus_annotations
WHERE project_id = @project_id
  AND file_path = @file_path
  AND deleted IS FALSE
ORDER BY created_at ASC;

-- name: DeleteAnnotation :one
UPDATE corpus_annotations
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;
