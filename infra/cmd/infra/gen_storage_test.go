package infra

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestGenStorage_InvalidInputPreservesArtifacts(t *testing.T) {
	// urfave/cli mutates shared help flags, so CLI invocations run sequentially.
	for _, tc := range []struct {
		name     string
		raw      []byte
		emptyOut bool
		want     string
	}{
		{name: "empty descriptors", want: "descriptors are empty"},
		{name: "no descriptor files", raw: []byte{0x10, 0x01}, want: "descriptors contain no files"},
		{name: "empty output", emptyOut: true, want: "--out must not be empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			descriptors := filepath.Join(dir, "descriptors.pb")
			require.NoError(t, os.WriteFile(descriptors, tc.raw, 0o600))
			for _, name := range []string{"storage_gen.go", "storage_manifest.json"} {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("existing artifact"), 0o644))
			}

			out := dir
			if tc.emptyOut {
				out = ""
			}

			app := &cli.App{Commands: []*cli.Command{newGenStorageCommand()}}
			err := app.RunContext(t.Context(), []string{"infra", "gen-storage", "--descriptors", descriptors, "--out=" + out})
			require.ErrorContains(t, err, tc.want)

			for _, name := range []string{"storage_gen.go", "storage_manifest.json"} {
				got, err := os.ReadFile(filepath.Join(dir, name))
				require.NoError(t, err)
				require.Equal(t, "existing artifact", string(got))
			}
		})
	}
}

func TestGenStorage_NoSubscriptionsWritesReadableArtifacts(t *testing.T) {
	// urfave/cli mutates shared help flags, so CLI invocations run sequentially.
	dir := t.TempDir()
	raw, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{
		Name: new("empty.proto"), Syntax: new("proto3"), Package: new("empty"),
		Options: &descriptorpb.FileOptions{GoPackage: new("example.com/empty")},
	}}})
	require.NoError(t, err)

	descriptors := filepath.Join(dir, "descriptors.pb")
	require.NoError(t, os.WriteFile(descriptors, raw, 0o600))

	app := &cli.App{Commands: []*cli.Command{newGenStorageCommand()}}
	require.NoError(t, app.RunContext(t.Context(), []string{"infra", "gen-storage", "--descriptors", descriptors, "--out", dir}))

	for _, name := range []string{"storage_gen.go", "storage_manifest.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	}
}
