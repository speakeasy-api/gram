package mcpregistry

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"net/url"
	"strings"
	"testing"
)

func TestSetCatalogIconsTxRollbackAndRevision(t *testing.T) {
	t.Parallel()

	ctx, s, db := newTestService(t)
	original, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	base, err := url.Parse("https://assets.example")
	require.NoError(t, err)
	s.WithAssetBaseURL(base)
	asset, err := assetrepo.New(db).CreatePlatformAsset(ctx, assetrepo.CreatePlatformAssetParams{Name: "icon.svg", Url: "icon.svg", Sha256: "icon", Kind: "image", ContentType: "image/svg+xml", ContentLength: 10})
	require.NoError(t, err)
	icons := CatalogIcons{Default: &CatalogIcon{Src: assets.ServeImageURL(base, asset.ID), MimeType: "image/svg+xml", Sizes: []string{"any"}}}
	tx := testenv.BeginTx(t, ctx, db)
	updated, err := s.SetCatalogIconsTx(ctx, tx, original.ID, Token(original), icons)
	require.NoError(t, err)
	got, err := ParseCatalogIcons(updated.Data)
	require.NoError(t, err)
	require.Equal(t, icons, got)
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

	tx = testenv.BeginTx(t, ctx, db)
	updated, err = s.SetCatalogIconsTx(ctx, tx, original.ID, Token(original), icons)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	actual, err = s.Get(ctx, original.ID)
	require.NoError(t, err)
	require.Equal(t, Token(updated), Token(actual))
	got, err = ParseCatalogIcons(actual.Data)
	require.NoError(t, err)
	require.Equal(t, icons, got)
}

func TestCatalogIconsPrepareSizeAdmission(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	id := uuid.New()
	prefix := strings.TrimSuffix(basicRecord, "}") + `,"numbers":[` + strings.Repeat("0,", 1024) + `0],"padding":"`
	size, err := repo.New(db).SerializedRegistryRecordBytes(ctx, []byte(prefix+`"}`))
	require.NoError(t, err)
	raw := prefix + strings.Repeat("x", StoredRecordByteLimit-int(size)) + `"}`
	require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(raw), Published: false}))
	old, err := s.Get(ctx, id)
	require.NoError(t, err)
	base, err := url.Parse("https://assets.example")
	require.NoError(t, err)
	s.WithAssetBaseURL(base)
	asset, err := assetrepo.New(db).CreatePlatformAsset(ctx, assetrepo.CreatePlatformAssetParams{Name: "icon.svg", Url: "icon.svg", Sha256: "icon", Kind: "image", ContentType: "image/svg+xml", ContentLength: 10})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, db)
	_, err = s.CheckCatalogIconsTx(ctx, tx, id, Token(old), CatalogIcons{Default: &CatalogIcon{Src: assets.ServeImageURL(base, asset.ID), MimeType: "image/svg+xml", Sizes: []string{"any"}}})
	var invalid *InvalidError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, storedSizeError(), invalid)
}

func TestCatalogIconsPublishRevalidatesLegacyMetadata(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	base, err := url.Parse("https://assets.example")
	require.NoError(t, err)
	s.WithAssetBaseURL(base)
	id := uuid.New()
	raw, err := WithCatalogIcons(json.RawMessage(basicRecord), CatalogIcons{Default: &CatalogIcon{Src: "https://untrusted.example/icon.svg", MimeType: "image/svg+xml", Sizes: []string{"any"}}})
	require.NoError(t, err)
	require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: raw, Published: false}))
	old, err := s.Get(ctx, id)
	require.NoError(t, err)
	_, err = s.SetPublished(ctx, id, Token(old), true)
	var invalid *InvalidError
	require.ErrorAs(t, err, &invalid)
	retained, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, Token(old), Token(retained))
	require.False(t, retained.Published)
}
