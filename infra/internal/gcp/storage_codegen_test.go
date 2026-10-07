package gcp

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestStorageCodegen_CommittedArtifactsAndDescriptorOrder(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../storagefixture/descriptors.pb")
	require.NoError(t, err)
	code, manifest, err := RenderStorage(raw, "storagefixture", "github.com/speakeasy-api/gram/infra/internal/storagefixture")
	require.NoError(t, err)
	for path, want := range map[string][]byte{"storage_gen.go": code, "storage_manifest.json": manifest} {
		got, err := os.ReadFile("../storagefixture/" + path)
		require.NoError(t, err)
		require.Equal(t, string(want), string(got), "regenerate %s", path)
	}
	var set descriptorpb.FileDescriptorSet
	require.NoError(t, proto.Unmarshal(raw, &set))
	// Source declaration order does not change logical column order or fingerprints.
	for _, file := range set.File {
		for _, message := range file.MessageType {
			slices.Reverse(message.Field)
		}
	}
	raw, err = proto.Marshal(&set)
	require.NoError(t, err)
	reorderedCode, reorderedManifest, err := RenderStorage(raw, "storagefixture", "github.com/speakeasy-api/gram/infra/internal/storagefixture")
	require.NoError(t, err)
	require.Equal(t, string(manifest), string(reorderedManifest))
	// protogen preserves source ordering for oneof alternatives, so code is
	// semantically identical even if the generated presence checks move.
	require.NotEmpty(t, reorderedCode)
}

func TestStorageCodegen_ReservedAndCaseInsensitiveColumns(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"__pubsub", "__present", "__oneof_choice", "part__day", "PART__year"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			set, payload, _, _ := storageFixture(t)
			payload.Field[0].Name = new(name)
			for _, file := range set.File {
				if file.Options == nil {
					file.Options = &descriptorpb.FileOptions{GoPackage: new("example.com/fixture;fixture")}
				}
			}
			raw, err := proto.Marshal(set)
			require.NoError(t, err)
			_, _, err = RenderStorage(raw, "fixture", "example.com/fixture")
			require.ErrorContains(t, err, "reserved or collides")
		})
	}
}

func TestStorageCodegen_FingerprintsIncludeFieldNumbers(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../storagefixture/descriptors.pb")
	require.NoError(t, err)
	_, before, err := RenderStorage(raw, "fixture", "example.com/fixture")
	require.NoError(t, err)
	var set descriptorpb.FileDescriptorSet
	require.NoError(t, proto.Unmarshal(raw, &set))
	for _, file := range set.File {
		if file.GetName() == "fixture/v1/event.proto" {
			file.MessageType[0].Field[0].Number = new(int32(100))
		}
	}
	raw, err = proto.Marshal(&set)
	require.NoError(t, err)
	_, after, err := RenderStorage(raw, "fixture", "example.com/fixture")
	require.NoError(t, err)
	var oldManifest, newManifest storageManifest
	require.NoError(t, json.Unmarshal(before, &oldManifest))
	require.NoError(t, json.Unmarshal(after, &newManifest))
	require.Equal(t, oldManifest.Subscriptions[1].Schema, newManifest.Subscriptions[1].Schema)
	require.NotEqual(t, oldManifest.Subscriptions[1].Fingerprint, newManifest.Subscriptions[1].Fingerprint)
}
