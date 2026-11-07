package mcpregistry

import (
	"context"
	"encoding/json"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"strings"
	"testing"
)

type refreshAssetProbe struct {
	CatalogRefreshAssets
	writes      int
	reads       int
	beforeWrite func(context.Context) error
}

func (p *refreshAssetProbe) ReadCatalogAsset(ctx context.Context, url string) ([]byte, error) {
	p.reads++
	data, err := p.CatalogRefreshAssets.ReadCatalogAsset(ctx, url)
	return data, refreshError(err)
}

func (p *refreshAssetProbe) StoreCatalogAsset(ctx context.Context, data []byte) (string, error) {
	p.writes++
	if p.beforeWrite != nil {
		if err := p.beforeWrite(ctx); err != nil {
			return "", err
		}
	}
	url, err := p.CatalogRefreshAssets.StoreCatalogAsset(ctx, data)
	return url, refreshError(err)
}
func TestCatalogRefreshAssetPreflightAndTokens(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	base, err := ParseAssetBaseURL("https://destination.example.test")
	require.NoError(t, err)
	s.WithAssetBaseURL(base)
	logger := testenv.NewLogger(t)
	tp := otel.GetTracerProvider()
	blob := assetstest.NewTestBlobStore(t)
	store := &CatalogAssetStore{Registry: s, Images: assets.NewPlatformService(logger, tp, guardian.NewDefaultPolicy(tp), db, blob), Blob: blob, Logger: logger, Operator: "operator@example.test"}
	probe := &refreshAssetProbe{CatalogRefreshAssets: store}
	const sourceURL = "https://source.example.test/icon.svg"
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"></rect></svg>`)
	raw, err := WithCatalogIcons(json.RawMessage(basicRecord), CatalogIcons{Default: &CatalogIcon{Src: sourceURL, MimeType: "image/svg+xml", Sizes: []string{"any"}}})
	require.NoError(t, err)
	upstream := CatalogBundle{Version: 1, Entries: []json.RawMessage{raw}, Assets: map[string][]byte{sourceURL: svg}}
	r := NewCatalogRefresher(s, "local", "production", refreshSourceFunc(func(context.Context) (CatalogBundle, error) { return upstream, nil }), probe)
	plan, err := r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Zero(t, probe.writes, "preview must not upload assets")
	require.Zero(t, probe.reads, "source bytes are already in the bundle; empty destination needs no asset reads")
	_, err = s.GetByName(ctx, "example.test/demo")
	require.ErrorIs(t, err, ErrNotFound, "preview must not commit catalog writes")
	// A relocated source URL with identical bytes is the same semantic snapshot.
	const movedURL = "https://source.example.test/relocated.svg"
	raw = json.RawMessage(strings.Replace(string(raw), sourceURL, movedURL, 1))
	upstream.Entries[0] = raw
	upstream.Assets = map[string][]byte{movedURL: svg}
	receipt, err := r.Apply(ctx, "owner", plan.ID)
	require.NoError(t, err)
	require.Equal(t, plan.SourceRevision, receipt.SourceRevision)
	require.Equal(t, 1, probe.writes)
	plan, err = r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "preserved", plan.Changes[0].Action, "destination asset ID must not count as local edits")
	upstream.Entries[0] = json.RawMessage(strings.Replace(string(raw), "Demo", "Updated", 1))
	plan, err = r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "update", plan.Changes[0].Action, plan.Changes)
	// Unlike a URL relocation, changed bytes invalidate this exact preview.
	upstream.Assets[movedURL] = []byte(strings.Replace(string(svg), `width="10"`, `width="9"`, 1))
	_, err = r.Apply(ctx, "owner", plan.ID)
	require.ErrorIs(t, err, ErrConflict)
	upstream.Assets[movedURL] = svg
	probe.beforeWrite = func(ctx context.Context) error {
		e, err := s.GetByName(ctx, "example.test/demo")
		if err != nil {
			return err
		}
		_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(strings.Replace(string(e.Data), "Demo", "Local edit", 1)))
		return err
	}
	_, err = r.Apply(ctx, "owner", plan.ID)
	require.ErrorIs(t, err, ErrConflict, "asset upload runs outside locks and destination is revalidated afterward")
	got, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	require.Contains(t, string(got.Data), "Local edit")
	status, err := r.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, "failed", status.LastAttemptState)
	require.Equal(t, "stale_plan", status.Failure)
}

func TestCatalogRefreshRejectsInvalidIconsBeforeSideEffects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, image, mime string
		sizes             []string
	}{
		{"invalid SVG", "not an SVG", "image/svg+xml", []string{"any"}},
		{"wrong MIME", `<svg xmlns="http://www.w3.org/2000/svg"/>`, "image/png", []string{"any"}},
		{"wrong sizes", `<svg xmlns="http://www.w3.org/2000/svg"/>`, "image/svg+xml", []string{"32x32"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, s, _ := newTestService(t)
			const url = "https://source.example.test/icon.svg"
			raw, err := WithCatalogIcons(json.RawMessage(basicRecord), CatalogIcons{Default: &CatalogIcon{Src: url, MimeType: tc.mime, Sizes: tc.sizes}})
			require.NoError(t, err)
			probe := &refreshAssetProbe{}
			r := NewCatalogRefresher(s, "local", "production", refreshSourceFunc(func(context.Context) (CatalogBundle, error) {
				return CatalogBundle{Version: 1, Entries: []json.RawMessage{raw}, Assets: map[string][]byte{url: []byte(tc.image)}}, nil
			}), probe)
			_, err = r.Preview(ctx, "owner")
			require.ErrorContains(t, err, "catalog source snapshot is invalid")
			require.Zero(t, probe.writes)
			require.Zero(t, probe.reads)
			_, err = s.GetByName(ctx, "example.test/demo")
			require.ErrorIs(t, err, ErrNotFound)
		})
	}
}
