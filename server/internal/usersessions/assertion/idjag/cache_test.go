package idjag

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

func TestIssuerKeyCacheRejectsLatePreRotationWrite(t *testing.T) {
	t.Parallel()

	db := newIssuerCacheTestDB(t)
	ctx := t.Context()
	uri := "https://issuer.example.com/jwks"
	id, err := remotesessionsrepo.New(db).CreateTestTrustedIssuerJWKSCache(ctx, remotesessionsrepo.CreateTestTrustedIssuerJWKSCacheParams{
		Slug: "issuer-" + uuid.NewString(), Issuer: "https://issuer.example.com", JwksUri: pgtype.Text{String: uri, Valid: true},
	})
	require.NoError(t, err)
	cache, err := NewIssuerKeyCache(db)
	require.NoError(t, err)
	key := issuerCacheKey(id, uri)
	prior, err := cache.Get(ctx, key)
	require.NoError(t, err)

	base := time.Now().UTC()
	newer := jwks.CacheState{Document: []byte(`{"keys":[{"kid":"rotated"}]}`), ETag: `"rotated"`, ExpiresAt: base.Add(time.Hour), RefreshedAt: base}
	older := jwks.CacheState{Document: []byte(`{"keys":[{"kid":"old"}]}`), ETag: `"old"`, ExpiresAt: base.Add(time.Hour), RefreshedAt: base.Add(time.Second)}
	written, err := cache.PutIfUnchanged(ctx, key, prior, newer)
	require.NoError(t, err)
	require.True(t, written)
	written, err = cache.PutIfUnchanged(ctx, key, prior, older)
	require.NoError(t, err)
	require.False(t, written)
	stored, err := cache.Get(ctx, key)
	require.NoError(t, err)
	require.JSONEq(t, string(newer.Document), string(stored.Document))
}
