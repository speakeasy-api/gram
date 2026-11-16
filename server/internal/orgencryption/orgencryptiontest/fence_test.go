package orgencryptiontest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
)

const fenceQueries = `
-- name: GetSecret :one
SELECT secret_encrypted, secret_org_encrypted FROM widgets WHERE id = @id;

-- name: LockSecret :one
SELECT secret_encrypted FROM widgets WHERE id = @id FOR UPDATE;

-- name: SetSecretFenced :exec
UPDATE widgets
SET secret_encrypted = @platform, secret_org_encrypted = @org
WHERE id = @id
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = widgets.organization_id AND t.use_case = 'widget-secrets'
  ), 0) = @generation;

-- name: SetSecretUnfenced :exec
-- A fence in a comment does not count:
-- AND COALESCE((SELECT generation FROM organization_encryption_transitions), 0) = @generation
UPDATE widgets SET secret_encrypted = @platform WHERE id = @id;

-- name: SetSecretWithoutCoalesce :exec
UPDATE widgets SET secret_org_encrypted = @org
WHERE id = @id
  AND (SELECT t.generation FROM organization_encryption_transitions AS t
       WHERE t.organization_id = widgets.organization_id AND t.use_case = 'widget-secrets') = @generation;

-- name: InsertWidget :one
INSERT INTO widgets (id, secret_org_encrypted) VALUES (@id, @org) RETURNING id;

-- name: UpsertWidget :exec
INSERT INTO widgets AS w (id, name) VALUES (@id, @name)
ON CONFLICT (id) DO UPDATE SET secret_encrypted = NULL;

-- name: EraseSecret :exec
-- orgencryption:unfenced erasing both copies is safe under any generation
UPDATE widgets SET secret_encrypted = NULL, secret_org_encrypted = NULL WHERE id = @id;

-- name: RenameWidget :exec
UPDATE widgets SET name = @name WHERE id = @id;
`

func TestUnfencedWrites_ReportsWritesWithoutTheFence(t *testing.T) {
	t.Parallel()

	got := orgencryptiontest.UnfencedWrites(fenceQueries, "secret_encrypted", "secret_org_encrypted")

	require.Equal(t, []string{"SetSecretUnfenced", "SetSecretWithoutCoalesce", "InsertWidget", "UpsertWidget"}, got)
}

func TestUnfencedWrites_MatchesWholeColumnNames(t *testing.T) {
	t.Parallel()

	got := orgencryptiontest.UnfencedWrites(fenceQueries, "secret")

	require.Empty(t, got)
}
