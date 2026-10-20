package naming_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/plugins/naming"
)

func publishedSnapshot(t *testing.T, name string) []byte {
	t.Helper()
	snapshot, err := naming.WithPublishedMarketplaceName([]byte(`{"marketplace_name":"hooks-name","org_name":"Acme"}`), name)
	require.NoError(t, err)
	return snapshot
}

func TestResolveMarketplaceName_ComputesNameBeforeFirstPublish(t *testing.T) {
	t.Parallel()

	require.Equal(t, "acme-corp-speakeasy", naming.ResolveMarketplaceName("", nil, "Acme Corp", "tools", true))
	require.Equal(t, "acme-corp-tools-speakeasy", naming.ResolveMarketplaceName("", nil, "Acme Corp", "tools", false))
}

func TestResolveMarketplaceName_PublishedNameIsFrozen(t *testing.T) {
	t.Parallel()

	snapshot := publishedSnapshot(t, "acme-speakeasy")

	// A renamed org, a changed slug, and a flipped default-project flag all
	// change the computed name, but none of them moves a published one.
	require.Equal(t, "acme-speakeasy", naming.ResolveMarketplaceName("", snapshot, "Renamed Corp", "tools", true))
	require.Equal(t, "acme-speakeasy", naming.ResolveMarketplaceName("", snapshot, "Acme", "renamed-slug", false))
	require.Equal(t, "acme-speakeasy", naming.ResolveMarketplaceName("", snapshot, "Acme", "tools", false))
}

func TestResolveMarketplaceName_OverrideWins(t *testing.T) {
	t.Parallel()

	snapshot := publishedSnapshot(t, "acme-speakeasy")

	require.Equal(t, "team-tools", naming.ResolveMarketplaceName("team-tools", snapshot, "Acme", "tools", true))
	require.Equal(t, "team-tools", naming.ResolveMarketplaceName("team-tools", nil, "Acme", "tools", true))
}

func TestResolveMarketplaceName_IgnoresHooksMarketplaceNameField(t *testing.T) {
	t.Parallel()

	// A snapshot written before published names were recorded carries only the
	// hooks config's marketplace_name, which the rollout gate can leave behind
	// the live manifests. It must not be read as the published name.
	legacy := []byte(`{"marketplace_name":"stale-speakeasy","org_name":"Acme"}`)

	require.Empty(t, naming.PublishedMarketplaceName(legacy))
	require.Equal(t, "acme-speakeasy", naming.ResolveMarketplaceName("", legacy, "Acme", "tools", true))
}

func TestPublishedMarketplaceName_EmptyForMissingOrUnreadableSnapshot(t *testing.T) {
	t.Parallel()

	for name, snapshot := range map[string][]byte{
		"nil":        nil,
		"empty":      {},
		"json null":  []byte(`null`),
		"not json":   []byte(`{`),
		"not object": []byte(`["acme-speakeasy"]`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Empty(t, naming.PublishedMarketplaceName(snapshot))
		})
	}
}

func TestDefaultMarketplaceName_SkipsNamePublishedByCurrentOverride(t *testing.T) {
	t.Parallel()

	snapshot := publishedSnapshot(t, "team-tools")

	// The recorded name is the override itself, so the project's default (what
	// clearing the override returns to) is the computed name.
	require.Equal(t, "renamed-corp-speakeasy", naming.DefaultMarketplaceName("team-tools", snapshot, "Renamed Corp", "tools", true))
	require.Equal(t, "team-tools", naming.ResolveMarketplaceName("team-tools", snapshot, "Renamed Corp", "tools", true))
}

func TestDefaultMarketplaceName_KeepsNameTheOverrideHasNotReplaced(t *testing.T) {
	t.Parallel()

	snapshot := publishedSnapshot(t, "acme-speakeasy")

	// The override has not published yet, so the repo still holds the recorded
	// name and clearing the override keeps it.
	require.Equal(t, "acme-speakeasy", naming.DefaultMarketplaceName("team-tools", snapshot, "Renamed Corp", "tools", true))
}

func TestWithPublishedMarketplaceName_KeepsOtherFieldsVerbatim(t *testing.T) {
	t.Parallel()

	original := []byte(`{"marketplace_name":"hooks-name","org_name":"Acme","binary_targets":{"linux":{"sha256":"abc"}},"published_marketplace_name":"old-name"}`)

	stamped, err := naming.WithPublishedMarketplaceName(original, "acme-speakeasy")
	require.NoError(t, err)
	require.Equal(t, "acme-speakeasy", naming.PublishedMarketplaceName(stamped))
	require.Equal(t, "Acme", naming.PublishedHooksOrgName(stamped))

	var before, after map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(original, &before))
	require.NoError(t, json.Unmarshal(stamped, &after))
	delete(before, naming.PublishedMarketplaceNameKey)
	delete(after, naming.PublishedMarketplaceNameKey)
	require.Equal(t, before, after)
}

func TestWithPublishedMarketplaceName_RecordsNameWithoutSnapshot(t *testing.T) {
	t.Parallel()

	for name, snapshot := range map[string][]byte{
		"nil":       nil,
		"json null": []byte(`null`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stamped, err := naming.WithPublishedMarketplaceName(snapshot, "acme-speakeasy")
			require.NoError(t, err)
			require.JSONEq(t, `{"published_marketplace_name":"acme-speakeasy"}`, string(stamped))
		})
	}
}

func TestWithPublishedMarketplaceName_RejectsNonObjectSnapshot(t *testing.T) {
	t.Parallel()

	_, err := naming.WithPublishedMarketplaceName([]byte(`["acme"]`), "acme-speakeasy")
	require.Error(t, err)
}
