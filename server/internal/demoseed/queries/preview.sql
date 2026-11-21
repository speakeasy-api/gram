-- Pull request preview fixtures (RunPreviewFixtures). Never part of the tenant
-- seed phases.

-- name: PreviewCurrentDatabase :one
-- The name of the database this connection writes to. A preview's is
-- gram_preview_pr_<N>; the server decides from it whether it is a preview.
SELECT current_database()::text;

-- name: PreviewUpsertRunner :execrows
-- The one account an end-to-end test run signs in as. Never platform staff.
-- It only ever creates the runner or restores its own row: an address that
-- already belongs to another user is a unique violation, and an id that
-- already belongs to another address updates nothing, which the caller treats
-- as a refusal. Neither adopts or rewrites somebody else.
INSERT INTO users (id, email, display_name, workos_id, admin)
VALUES (@id::text, @email::text, @display_name::text, @workos_id::text, FALSE)
ON CONFLICT (id) DO UPDATE
  SET display_name = EXCLUDED.display_name, workos_id = EXCLUDED.workos_id,
      admin = FALSE, deleted_at = NULL, updated_at = clock_timestamp()
  WHERE users.email = EXCLUDED.email;
