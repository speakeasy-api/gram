package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"strings"
	"testing"
)

func TestCatalogTransferRoundTrip(t *testing.T) {
	t.Parallel()
	ctx, source, _ := newTestService(t)
	_, err := source.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	hidden, err := source.Create(ctx, json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/hidden", 1)))
	require.NoError(t, err)
	_, err = source.SetPublished(ctx, hidden.ID, Token(hidden), false)
	require.NoError(t, err)
	bundle, err := source.ExportCatalog(ctx, nil)
	require.NoError(t, err)
	require.Len(t, bundle.Entries, 1)
	_, dest, _ := newTestService(t)
	transfer := NewCatalogImporter(dest, "local", nil)
	result, err := transfer.Import(ctx, bundle)
	require.NoError(t, err)
	require.Equal(t, []string{"example.test/demo"}, result.Created)
	result, err = transfer.Import(ctx, bundle)
	require.NoError(t, err)
	require.Equal(t, []string{"example.test/demo"}, result.Conflicts)
	entry, err := dest.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	require.True(t, entry.Published)
	require.NotEqual(t, string(bundle.Entries[0]), string(entry.Data))
}

func TestCatalogImportRejectsBeforeWrites(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	bundle := CatalogBundle{Version: 1, Entries: []json.RawMessage{json.RawMessage(basicRecord), json.RawMessage(`{}`)}}
	_, err := NewCatalogImporter(s, "local", nil).Import(ctx, bundle)
	require.Error(t, err)
	_, err = s.GetByName(ctx, "example.test/demo")
	require.ErrorIs(t, err, ErrNotFound)
	for _, env := range []string{"production", "prod", "", "preview"} {
		_, err := NewCatalogImporter(nil, env, nil).Import(ctx, CatalogBundle{})
		require.ErrorContains(t, err, "destination")
	}
}

func TestCatalogTransferLogoBytesAndEditedConflict(t *testing.T) {
	t.Parallel()
	ctx, source, sourceDB := newTestService(t)
	_, dest, destDB := newTestService(t)
	sourceBase, err := ParseAssetBaseURL("https://source.example.test")
	require.NoError(t, err)
	destBase, err := ParseAssetBaseURL("https://destination.example.test")
	require.NoError(t, err)
	source.WithAssetBaseURL(sourceBase)
	dest.WithAssetBaseURL(destBase)
	logger := testenv.NewLogger(t)
	tp := otel.GetTracerProvider()
	sourceBlob := assetstest.NewTestBlobStore(t)
	destBlob := assetstest.NewTestBlobStore(t)
	sourceStore := &CatalogAssetStore{Registry: source, Images: assets.NewPlatformService(logger, tp, guardian.NewDefaultPolicy(tp), sourceDB, sourceBlob), Blob: sourceBlob, Logger: logger, Operator: "operator@example.test"}
	destStore := &CatalogAssetStore{Registry: dest, Images: assets.NewPlatformService(logger, tp, guardian.NewDefaultPolicy(tp), destDB, destBlob), Blob: destBlob, Logger: logger, Operator: "operator@example.test"}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="#123456"/></svg>`)
	src, err := sourceStore.StoreCatalogAsset(ctx, svg)
	require.NoError(t, err)
	icon := &CatalogIcon{Src: src, MimeType: "image/svg+xml", Sizes: []string{"any"}}
	raw, err := WithCatalogIcons(json.RawMessage(basicRecord), CatalogIcons{Default: icon, Dark: icon})
	require.NoError(t, err)
	original, err := source.Create(ctx, raw)
	require.NoError(t, err)
	bundle, err := source.ExportCatalog(ctx, sourceStore)
	require.NoError(t, err)
	require.Len(t, bundle.Assets, 1)
	encoded, err := json.Marshal(bundle)
	require.NoError(t, err)
	bundle, err = DecodeCatalogBundle(strings.NewReader(string(encoded)))
	require.NoError(t, err)
	result, err := NewCatalogImporter(dest, "dev", destStore).Import(ctx, bundle)
	require.NoError(t, err)
	require.Len(t, result.Created, 1)
	copied, err := dest.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	icons, err := ParseCatalogIcons(copied.Data)
	require.NoError(t, err)
	require.NotEqual(t, src, icons.Default.Src)
	require.Contains(t, icons.Default.Src, "destination.example.test")
	actual, err := destStore.ReadCatalogAsset(ctx, icons.Default.Src)
	require.NoError(t, err)
	require.Equal(t, bundle.Assets[src], actual)
	edited, err := dest.Save(ctx, copied.ID, Token(copied), json.RawMessage(strings.Replace(string(copied.Data), `"Demo"`, `"Locally edited"`, 1)))
	require.NoError(t, err)
	result, err = NewCatalogImporter(dest, "dev", destStore).Import(ctx, bundle)
	require.NoError(t, err)
	require.Len(t, result.Conflicts, 1)
	retained, err := dest.Get(ctx, edited.ID)
	require.NoError(t, err)
	require.Equal(t, edited.Data, retained.Data)
	unchanged, err := source.Get(ctx, original.ID)
	require.NoError(t, err)
	require.Equal(t, original.Data, unchanged.Data)
	require.Equal(t, Token(original), Token(unchanged))
}

type countingCatalogWriter struct{ calls int }

func (w *countingCatalogWriter) StoreCatalogAsset(context.Context, []byte) (string, error) {
	w.calls++
	return "", nil
}
func TestCatalogBundleInvalidAssets(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	icon := &CatalogIcon{Src: "https://source.example.test/logo", MimeType: "image/svg+xml", Sizes: []string{"any"}}
	raw, err := WithCatalogIcons(json.RawMessage(basicRecord), CatalogIcons{Default: icon})
	require.NoError(t, err)
	for _, data := range [][]byte{nil, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)} {
		w := &countingCatalogWriter{}
		_, err := NewCatalogImporter(s, "local", w).Import(ctx, CatalogBundle{Version: 1, Entries: []json.RawMessage{raw}, Assets: map[string][]byte{icon.Src: data}})
		require.Error(t, err)
		require.Zero(t, w.calls)
	}
}

func TestCatalogBundleDecoding(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{"version":1,"unexpected":true}`, `{"version":1} {}`, `{"version":1}garbage`} {
		_, err := DecodeCatalogBundle(strings.NewReader(raw))
		require.Error(t, err)
	}
	ctx, s, _ := newTestService(t)
	_, err := NewCatalogImporter(s, "local", nil).Import(ctx, CatalogBundle{Version: 2})
	require.ErrorContains(t, err, "version")
	_, err = NewCatalogImporter(s, "local", nil).Import(ctx, CatalogBundle{Version: 1, Entries: []json.RawMessage{json.RawMessage(basicRecord), json.RawMessage(basicRecord)}})
	require.ErrorContains(t, err, "duplicate")
}

func TestCatalogTransferPreservesUnknownMetadata(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	raw := json.RawMessage(strings.TrimSuffix(basicRecord, "}") + `,"_meta":{"example.test/custom":{"precise":9007199254740993,"nested":{"enabled":true}},"com.speakeasy.ai/registry":{"publishedAt":"2020-01-01T00:00:00Z"}}}`)
	_, err := NewCatalogImporter(s, "local", nil).Import(ctx, CatalogBundle{Version: 1, Entries: []json.RawMessage{raw}})
	require.NoError(t, err)
	e, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	require.Contains(t, string(e.Data), "9007199254740993")
	require.NotContains(t, string(e.Data), "2020-01-01")
}

func TestCatalogImportRollsBackLateBusinessConflict(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	_, err := s.Create(ctx, json.RawMessage(oktaRecord("example.test/existing", "synthetic-app")))
	require.NoError(t, err)
	bundle := CatalogBundle{Version: 1, Entries: []json.RawMessage{json.RawMessage(basicRecord), json.RawMessage(oktaRecord("example.test/new", "synthetic-app"))}}
	_, err = NewCatalogImporter(s, "local", nil).Import(ctx, bundle)
	require.Error(t, err)
	_, err = s.GetByName(ctx, "example.test/demo")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestCatalogBundleValidatesSharedAssetOnce(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	icon := &CatalogIcon{Src: "https://source.example.test/shared.svg", MimeType: "image/svg+xml", Sizes: []string{"any"}}
	bundle := CatalogBundle{Version: 1, Assets: map[string][]byte{icon.Src: make([]byte, 4<<20)}}
	for n := range 10000 {
		raw := strings.Replace(basicRecord, "example.test/demo", fmt.Sprintf("example.test/item-%d", n), 1)
		record, err := WithCatalogIcons(json.RawMessage(raw), CatalogIcons{Default: icon, Dark: icon})
		require.NoError(t, err)
		bundle.Entries = append(bundle.Entries, record)
	}
	calls := 0
	err := s.validateCatalogBundle(ctx, bundle, func(data []byte) error { calls++; require.Len(t, data, 4<<20); return nil })
	require.NoError(t, err)
	require.Equal(t, 1, calls, "20,000 icon references must cause only one SVG validation")
}

func TestCatalogBundleValidationCancellation(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	calls := 0
	err := s.validateCatalogBundle(canceled, CatalogBundle{Version: 1}, func([]byte) error { calls++; return nil })
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls)

	icon := &CatalogIcon{Src: "https://source.example.test/shared.svg", MimeType: "image/svg+xml", Sizes: []string{"any"}}
	raw, err := WithCatalogIcons(json.RawMessage(basicRecord), CatalogIcons{Default: icon, Dark: icon})
	require.NoError(t, err)
	running, stop := context.WithCancel(ctx)
	defer stop()
	bundle := CatalogBundle{Version: 1, Entries: []json.RawMessage{raw}, Assets: map[string][]byte{icon.Src: []byte("image")}}
	err = s.validateCatalogBundle(running, bundle, func([]byte) error { calls++; stop(); return nil })
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}

func TestCatalogImportErrorContext(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	_, err := NewCatalogImporter(s, "local", nil).Import(ctx, CatalogBundle{Version: 999})
	require.ErrorContains(t, err, "validate import bundle: unsupported catalog bundle version")
	_, err = s.Create(ctx, json.RawMessage(oktaRecord("example.test/existing", "synthetic-conflict")))
	require.NoError(t, err)
	_, err = NewCatalogImporter(s, "local", nil).Import(ctx, CatalogBundle{Version: 1, Entries: []json.RawMessage{json.RawMessage(oktaRecord("example.test/new", "synthetic-conflict"))}})
	require.ErrorContains(t, err, "create imported catalog entry:")
}
