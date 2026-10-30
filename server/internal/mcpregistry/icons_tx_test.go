package mcpregistry

import (
	"encoding/json"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSetCatalogIconsTxRollbackAndRevision(t *testing.T) {
	t.Parallel()

	ctx, s, db := newTestService(t)
	original, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, db)
	updated, err := s.SetCatalogIconsTx(ctx, tx, original.ID, Token(original), CatalogIcons{})
	require.NoError(t, err)
	require.NotEqual(t, Token(original), Token(updated))
	require.Equal(t, original.Published, updated.Published)
	require.Equal(t, original.PublishedAt, updated.PublishedAt)
	_, err = s.SetCatalogIconsTx(ctx, tx, original.ID, Token(original), CatalogIcons{})
	require.ErrorIs(t, err, ErrConflict)
	require.NoError(t, tx.Rollback(ctx))
	actual, err := s.Get(ctx, original.ID)
	require.NoError(t, err)
	require.Equal(t, Token(original), Token(actual))
	require.JSONEq(t, string(original.Data), string(actual.Data))
}
