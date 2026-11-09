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

func TestCatalogResetAssetPreflightAndTokens(t *testing.T) {
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
	r := NewCatalogResetter(s, "local", "production", "local https://localhost:9090", refreshSourceFunc(func(context.Context) (CatalogBundle, error) { return upstream, nil }), probe)
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
	receipt, err := r.Apply(ctx, "owner", plan.ID, plan.Confirmation)
	require.NoError(t, err)
	require.Equal(t, plan.SourceRevision, receipt.SourceRevision)
	require.Equal(t, 1, probe.writes)

	// A testing logo and metadata edit are overwritten from production, while
	// the record identity and immutable endpoint are retained.
	original, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	testingSVG := []byte(strings.Replace(string(svg), `width="10"`, `width="8"`, 1))
	testingURL, err := store.StoreCatalogAsset(ctx, testingSVG)
	require.NoError(t, err)
	icons, err := ParseCatalogIcons(original.Data)
	require.NoError(t, err)
	icons.Default.Src = testingURL
	testingData, err := WithCatalogIcons(original.Data, icons)
	require.NoError(t, err)
	_, err = s.Save(ctx, original.ID, Token(original), testingData)
	require.NoError(t, err)
	writesBefore := probe.writes
	plan, err = r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, writesBefore, probe.writes)
	require.Equal(t, "update", plan.Changes[0].Action)
	_, err = r.Apply(ctx, "owner", plan.ID, plan.Confirmation)
	require.NoError(t, err)
	restored, err := s.Get(ctx, original.ID)
	require.NoError(t, err)
	icons, err = ParseCatalogIcons(restored.Data)
	require.NoError(t, err)
	restoredSVG, err := store.ReadCatalogAsset(ctx, icons.Default.Src)
	require.NoError(t, err)
	require.Equal(t, svg, restoredSVG)
	plan, err = r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "adopt", plan.Changes[0].Action, "destination asset ID must not count as local edits")
	upstream.Entries[0] = json.RawMessage(strings.Replace(string(raw), "Demo", "Updated", 1))
	plan, err = r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "update", plan.Changes[0].Action, plan.Changes)
	// Unlike a URL relocation, changed bytes invalidate this exact preview.
	upstream.Assets[movedURL] = []byte(strings.Replace(string(svg), `width="10"`, `width="9"`, 1))
	_, err = r.Apply(ctx, "owner", plan.ID, plan.Confirmation)
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
	_, err = r.Apply(ctx, "owner", plan.ID, plan.Confirmation)
	require.ErrorIs(t, err, ErrConflict, "asset upload runs outside locks and destination is revalidated afterward")
	got, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	require.Contains(t, string(got.Data), "Local edit")
	status, err := r.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, "failed", status.LastAttemptState)
	require.Equal(t, "stale_plan", status.Failure)
}
