package adminmcp

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/stretchr/testify/require"
	"net/url"
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
	icons := boundedCatalogIcons(data)
	require.NotNil(t, icons.Default)
	encoded, err := json.Marshal(icons)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
	require.NotContains(t, string(encoded), "private")
}
