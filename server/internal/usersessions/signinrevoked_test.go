package usersessions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

func TestSignInRevoked_ALookupFailureIsNotReadAsNotRevoked(t *testing.T) {
	t.Parallel()

	pool, err := pgxpool.New(t.Context(), "postgres://gram@127.0.0.1:1/gram?connect_timeout=1")
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	revoked, err := (&Service{
		aiCatalog: aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())),
		db:        pool}).signInRevoked(t.Context(), "org", uuid.NullUUID{UUID: uuid.New(), Valid: true})
	require.Error(t, err)
	require.False(t, revoked)
}
