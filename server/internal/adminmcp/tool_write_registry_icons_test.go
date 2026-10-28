package adminmcp

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/stretchr/testify/require"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRegistryIconsApprovalRevisionAndRetry(t *testing.T) {
	f := newProposalFixture(t, "admin_mcp_registry_icons")
	_, _, registry := newRegistryEntryWriter(t, f)
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetRegistryIcons: true}}
	writer := &registryIconsWriter{store: f.store, registry: registry, writes: writes, baseURL: "https://staff.example.test" + Path}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{OperationSetRegistryIcons: writer})
	original, err := registry.Create(t.Context(), json.RawMessage(registryRecord("example.test/"+uuid.NewString())))
	require.NoError(t, err)
	base, _ := url.Parse("https://assets.example.test")
	registry.WithAssetBaseURL(base)
	asset, err := assetrepo.New(f.db).CreatePlatformAsset(t.Context(), assetrepo.CreatePlatformAssetParams{Name: "icon.svg", Url: "icon.svg", Sha256: "icons-test", Kind: "image", ContentType: "image/svg+xml", ContentLength: 10})
	require.NoError(t, err)
	icon := &mcpregistry.CatalogIcon{Src: assets.ServeImageURL(base, asset.ID), MimeType: "image/svg+xml", Sizes: []string{"any"}}
	input := PrepareSetRegistryIconsInput{EntryID: original.ID.String(), ExpectedUpdatedAt: mcpregistry.Token(original), RetryKey: "icons-1", Icons: mcpregistry.CatalogIcons{Default: icon, Dark: icon}}
	_, err = writer.prepare(context.Background(), input)
	require.Error(t, err)
	for _, bad := range []PrepareSetRegistryIconsInput{{EntryID: "not-uuid", ExpectedUpdatedAt: input.ExpectedUpdatedAt, RetryKey: "bad"}, {EntryID: input.EntryID, RetryKey: "bad"}, {EntryID: input.EntryID, ExpectedUpdatedAt: input.ExpectedUpdatedAt}} {
		_, err = writer.prepare(writeContext(t, f), bad)
		require.Error(t, err)
	}
	invalid := input
	invalid.Icons.Default = &mcpregistry.CatalogIcon{Src: "https://private.example/icon.svg", MimeType: "image/svg+xml", Sizes: []string{"any"}}
	_, err = writer.prepare(writeContext(t, f), invalid)
	require.Error(t, err)
	prepared, err := writer.prepare(writeContext(t, f), input)
	require.NoError(t, err)
	replay, err := writer.prepare(writeContext(t, f), input)
	require.NoError(t, err)
	require.True(t, replay.Replay)
	_, err = tools.execute(writeContext(t, f), ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotApproved)
	id := uuid.MustParse(prepared.ProposalID)
	stored, err := f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	_, err = writer.view(stored)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), id, f.owner.SubjectURN, stored.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
	result, err := tools.execute(writeContext(t, f), ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	actual, err := registry.Get(t.Context(), original.ID)
	require.NoError(t, err)
	require.NotEqual(t, mcpregistry.Token(original), mcpregistry.Token(actual))
	gotIcons, err := mcpregistry.ParseCatalogIcons(actual.Data)
	require.NoError(t, err)
	require.Equal(t, input.Icons, gotIcons)
	cleared, err := mcpregistry.WithCatalogIcons(actual.Data, mcpregistry.CatalogIcons{})
	require.NoError(t, err)
	require.JSONEq(t, string(original.Data), string(cleared))
	replay, err = writer.prepare(writeContext(t, f), input)
	require.NoError(t, err)
	require.True(t, replay.Replay)
	input.ExpectedUpdatedAt = mcpregistry.Token(actual)
	input.RetryKey = "icons-stale"
	prepared, err = writer.prepare(writeContext(t, f), input)
	require.NoError(t, err)
	id = uuid.MustParse(prepared.ProposalID)
	stored, err = f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), id, f.owner.SubjectURN, stored.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
	_, err = registry.Save(t.Context(), actual.ID, mcpregistry.Token(actual), actual.Data)
	require.NoError(t, err)
	result, err = tools.execute(writeContext(t, f), ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrStaleState)
	require.NotEqual(t, string(ProposalSucceeded), result.Status)
}
func TestRegistryIconsBoundedProjection(t *testing.T) {
	data := json.RawMessage(`{"server":{"icons":[{"src":"private"}]},"_meta":{"private":"secret","com.speakeasy.ai/icons":{"default":{"src":"https://public.example/icon.svg","mimeType":"image/svg+xml","sizes":["any"]}}}}`)
	icons := boundedCatalogIcons(t.Context(), nil, data)
	require.Nil(t, icons.Default)
	encoded, err := json.Marshal(icons)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
	require.NotContains(t, string(encoded), "private")
}

type validatedRegistryReader struct {
	*recordingRegistryReader
	*mcpregistry.Service
}

func TestRegistryIconsTrustedProjection(t *testing.T) {
	f := newProposalFixture(t, "admin_mcp_registry_icon_projection")
	_, _, registry := newRegistryEntryWriter(t, f)
	base, err := url.Parse("https://assets.example")
	require.NoError(t, err)
	registry.WithAssetBaseURL(base)
	asset, err := assetrepo.New(f.db).CreatePlatformAsset(t.Context(), assetrepo.CreatePlatformAssetParams{Name: "icon.svg", Url: "icon.svg", Sha256: "projection-icon", Kind: "image", ContentType: "image/svg+xml", ContentLength: 10})
	require.NoError(t, err)
	canonical := assets.ServeImageURL(base, asset.ID)
	for _, tc := range []struct {
		name, src string
		want      bool
	}{
		{"canonical", canonical, true},
		{"arbitrary text", "private-value", false},
		{"userinfo", strings.Replace(canonical, "https://", "https://user:password@", 1), false},
		{"external", strings.Replace(canonical, "assets.example", "external.example", 1), false},
		{"noncanonical path", strings.Replace(canonical, "/rpc/", "/other/", 1), false},
		{"extra query", canonical + "&token=secret", false},
		{"duplicate id", canonical + "&id=" + asset.ID.String(), false},
		{"fragment", canonical + "#secret", false},
		{"missing asset", assets.ServeImageURL(base, uuid.New()), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Deliberately invalid stored record: icon safety is independent of schema projection.
			raw, err := mcpregistry.WithCatalogIcons(json.RawMessage(`{"_meta":{"private":"unrelated-secret"}}`), mcpregistry.CatalogIcons{
				Default: &mcpregistry.CatalogIcon{Src: tc.src, MimeType: "image/svg+xml", Sizes: []string{"any"}},
				Dark:    &mcpregistry.CatalogIcon{Src: canonical, MimeType: "image/svg+xml", Sizes: []string{"any"}},
			})
			require.NoError(t, err)
			reader := &validatedRegistryReader{recordingRegistryReader: &recordingRegistryReader{entry: &gen.AdminRegistryEntry{ID: testRegistryID, DataJSON: string(raw)}}, Service: registry}
			body, data := registryToolCall(t, reader, "get_registry_entry", `{"id":"`+testRegistryID+`"}`, true)
			var output RegistryEntryDetail
			require.NoError(t, json.Unmarshal(data, &output))
			require.True(t, output.Found)
			require.False(t, output.ProjectionAvailable)
			require.NotContains(t, body, "unrelated-secret")
			require.NotNil(t, output.CuratedIcons.Dark)
			require.Equal(t, canonical, output.CuratedIcons.Dark.Src)
			if tc.want {
				require.NotNil(t, output.CuratedIcons.Default)
			} else {
				require.Nil(t, output.CuratedIcons.Default)
				require.NotContains(t, body, tc.src)
			}
		})
	}
	icon := &mcpregistry.CatalogIcon{Src: canonical, MimeType: "image/svg+xml", Sizes: []string{"any"}}
	for _, assignment := range []string{"content_type = 'image/png'", "project_id = '00000000-0000-4000-8000-000000000001'", "organization_id = '" + f.orgA + "'", "deleted_at = clock_timestamp()"} {
		_, err = f.db.Exec(t.Context(), "UPDATE assets SET "+assignment+" WHERE id = $1", asset.ID) //nolint:glint // notestingrawsql: exercise unsafe legacy asset associations.
		require.NoError(t, err)
		require.False(t, registry.IsPublicCatalogIcon(t.Context(), icon))
		_, err = f.db.Exec(t.Context(), "UPDATE assets SET content_type = 'image/svg+xml', project_id = NULL, organization_id = NULL, deleted_at = NULL WHERE id = $1", asset.ID) //nolint:glint // notestingrawsql: restore the asset fixture.
		require.NoError(t, err)
	}
	registry.WithAssetBaseURL(nil)
	require.False(t, registry.IsPublicCatalogIcon(t.Context(), icon))
	registry.WithAssetBaseURL(base)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.False(t, registry.IsPublicCatalogIcon(ctx, icon))
}
