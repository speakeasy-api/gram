-- Pull request preview fixtures (RunPreviewFixtures). Never part of the tenant
-- seed phases.

-- name: PreviewCurrentDatabase :one
-- The name of the database this connection writes to. A preview's is
-- gram_preview_pr_<N>; the server decides from it whether it is a preview.
SELECT current_database()::text;

-- name: PreviewUpsertRunner :exec
-- The one account an end-to-end test run signs in as. Never platform staff,
-- and keyed on id only: an address that already belongs to another user is a
-- unique violation, which aborts the fixtures instead of adopting that user.
INSERT INTO users (id, email, display_name, workos_id, admin)
VALUES (@id::text, @email::text, @display_name::text, @workos_id::text, FALSE)
ON CONFLICT (id) DO UPDATE
  SET email = EXCLUDED.email, display_name = EXCLUDED.display_name,
      workos_id = EXCLUDED.workos_id, admin = FALSE, deleted_at = NULL,
      updated_at = clock_timestamp();
