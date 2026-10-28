package mcpregistry

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/stretchr/testify/require"
	"net/url"
	"strings"
	"testing"
)

func TestCatalogIconsRoundTrip(t *testing.T) {
	raw := json.RawMessage(`{"server":{"icons":[{"src":"https://upstream.example/icon.png"}]},"_meta":{"example.test/data":{"n":9007199254740993}}}`)
	icon := &CatalogIcon{Src: "https://assets.example/rpc/assets.serveImage?id=00000000-0000-4000-8000-000000000001", MimeType: "image/svg+xml", Sizes: []string{"any"}}
	for _, icons := range []CatalogIcons{{Default: icon}, {Default: icon, Dark: icon}, {Dark: icon}, {}} {
		updated, err := WithCatalogIcons(raw, icons)
		require.NoError(t, err)
		got, err := ParseCatalogIcons(updated)
		require.NoError(t, err)
		require.Equal(t, icons, got)
		require.Contains(t, string(updated), "9007199254740993")
		cleared, err := WithCatalogIcons(updated, CatalogIcons{})
		require.NoError(t, err)
		require.JSONEq(t, string(raw), string(cleared))
	}
}

func TestCatalogIconsAssociation(t *testing.T) {
	ctx, s, db := newTestService(t)
	base, _ := url.Parse("https://assets.example")
	s.WithAssetBaseURL(base)
	asset, err := assetrepo.New(db).CreatePlatformAsset(ctx, assetrepo.CreatePlatformAssetParams{Name: "icon.svg", Url: "icon.svg", Sha256: "icon", Kind: "image", ContentType: "image/svg+xml", ContentLength: 10})
	require.NoError(t, err)
	icon := &CatalogIcon{Src: assets.ServeImageURL(base, asset.ID), MimeType: "image/svg+xml", Sizes: []string{"any"}}
	raw, err := WithCatalogIcons(json.RawMessage(basicRecord), CatalogIcons{Default: icon, Dark: icon})
	require.NoError(t, err)
	e, err := s.Create(ctx, raw)
	require.NoError(t, err)
	page, err := s.Discover(ctx, DiscoveryOptions{})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	got, err := ParseCatalogIcons(page.Records[0])
	require.NoError(t, err)
	require.Equal(t, icon, got.Default)
	for _, src := range []string{strings.Replace(icon.Src, "assets.example", "evil.example", 1), icon.Src + "&extra=1", icon.Src + "&id=" + asset.ID.String(), icon.Src + "#fragment", strings.Replace(icon.Src, "https://", "http://", 1), "https://assets.example/rpc/assets.serveImage?id=" + uuid.Nil.String(), assets.ServeImageURL(base, uuid.New())} {
		bad, err := WithCatalogIcons(raw, CatalogIcons{Dark: &CatalogIcon{Src: src, MimeType: "image/svg+xml", Sizes: []string{"any"}}})
		require.NoError(t, err)
		_, err = s.Save(ctx, e.ID, Token(e), bad)
		require.Error(t, err)
	}
	for _, assignment := range []string{"content_type = 'image/png'", "project_id = '00000000-0000-4000-8000-000000000001'", "deleted_at = clock_timestamp()"} {
		_, err = db.Exec(ctx, "UPDATE assets SET "+assignment+" WHERE id = $1", asset.ID) //nolint:glint // notestingrawsql: exercise invalid asset association states.
		require.NoError(t, err)
		_, err = s.Save(ctx, e.ID, Token(e), raw)
		require.Error(t, err)
		_, err = db.Exec(ctx, "UPDATE assets SET content_type = 'image/svg+xml', project_id = NULL, deleted_at = NULL WHERE id = $1", asset.ID) //nolint:glint // notestingrawsql: restore the fixture after each invalid association.
		require.NoError(t, err)
	}
	retained, err := s.Get(ctx, e.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(e.Data), string(retained.Data))
	require.Equal(t, Token(e), Token(retained))
	saved, err := s.Save(ctx, e.ID, Token(e), raw)
	require.NoError(t, err)
	require.Equal(t, e.PublishedAt, saved.PublishedAt)
	_, err = s.Save(ctx, e.ID, Token(e), raw)
	require.ErrorIs(t, err, ErrConflict)
	_, err = s.Save(ctx, e.ID, Token(saved), json.RawMessage(strings.Replace(string(raw), "https://example.test/mcp", "https://example.test/changed", 1)))
	require.ErrorIs(t, err, ErrEndpointStructureImmutable)
	s.WithAssetBaseURL(nil)
	_, err = s.Save(ctx, e.ID, Token(saved), raw)
	require.Error(t, err)
	cleared, err := s.Save(ctx, e.ID, Token(saved), json.RawMessage(basicRecord))
	require.NoError(t, err)
	require.NotContains(t, string(cleared.Data), IconsNamespace)
}

func TestCatalogIconsSchema(t *testing.T) {
	v, err := LoadValidator()
	require.NoError(t, err)
	for _, value := range []string{`{"default":null}`, `{"light":{}}`, `{"default":{"src":"https://assets.example/icon","mimeType":"image/png","sizes":["any"]}}`, `{"default":{"src":"https://assets.example/icon","mimeType":"image/svg+xml","sizes":["any"],"theme":"dark"}}`} {
		raw := strings.TrimSuffix(basicRecord, "}") + `,"_meta":{"com.speakeasy.ai/icons":` + value + `}}`
		require.NotEmpty(t, v.Validate(json.RawMessage(raw)), value)
	}
}
