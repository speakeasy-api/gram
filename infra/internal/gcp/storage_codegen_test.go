package gcp

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
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
	require.Equal(t, string(code), string(reorderedCode))
}

func TestStorageCodegen_FingerprintIncludesOneofMembership(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../storagefixture/descriptors.pb")
	require.NoError(t, err)
	var set descriptorpb.FileDescriptorSet
	require.NoError(t, proto.Unmarshal(raw, &set))

	var moved *descriptorpb.FieldDescriptorProto
	for _, file := range set.File {
		if file.GetName() != "fixture/v1/event.proto" {
			continue
		}

		event := file.MessageType[0]
		event.OneofDecl = append(event.OneofDecl, &descriptorpb.OneofDescriptorProto{Name: new("other")})
		for _, field := range event.Field {
			if field.GetName() == "chosen_data" {
				field.OneofIndex = new(int32(1))
			}
			if field.GetName() == "chosen_child" {
				moved = field
			}
		}
	}
	require.NotNil(t, moved)

	raw, err = proto.Marshal(&set)
	require.NoError(t, err)
	_, before, err := RenderStorage(raw, "fixture", "example.com/fixture")
	require.NoError(t, err)

	moved.OneofIndex = new(int32(1))
	raw, err = proto.Marshal(&set)
	require.NoError(t, err)
	_, after, err := RenderStorage(raw, "fixture", "example.com/fixture")
	require.NoError(t, err)

	var oldManifest, newManifest storageManifest
	require.NoError(t, json.Unmarshal(before, &oldManifest))
	require.NoError(t, json.Unmarshal(after, &newManifest))
	old, updated := oldManifest.Subscriptions[1], newManifest.Subscriptions[1]
	require.Equal(t, old.Schema, updated.Schema)
	require.NotEqual(t, old.Fingerprint, updated.Fingerprint)
	require.Equal(t, StorageMappingVersion, newManifest.MappingVersion)

	hash := sha256.Sum256([]byte("mapping=" + newManifest.MappingVersion + "\n" + updated.Schema + "\n" + strings.Join(updated.Fields, "\n")))
	require.Equal(t, fmt.Sprintf("%x", hash), updated.Fingerprint)
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
