package assistants

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKitLocalAssertionDirectoryMountSupportsAtomicRotation(t *testing.T) {
	t.Parallel()
	spec := kitLocalContainerSpec{WorkloadAssertionFile: "/operator/projection/assertion", Command: []string{"kit"}, Image: "kit:v0.2.19"}
	args := kitLocalCreateArgs(8081, spec)
	require.Contains(t, args, "type=bind,src=/operator/projection,dst=/var/run/secrets/speakeasy/workload,readonly")
	require.NotContains(t, strings.Join(args, " "), "src=/operator/projection/assertion")
	require.NotContains(t, strings.Join(args, " "), "dst=/home/kit/.kit")
	// Replacing assertion changes a directory entry, not the mounted inode.
	require.Equal(t, "/var/run/secrets/speakeasy/workload", filepath.Dir(KitWorkloadAssertionPath))
}

func TestKitLocalAssertionProjectionRejectsUnsafePaths(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateKitLocalAssertionFile("/operator/projection/assertion"))
	for _, path := range []string{"", "relative/assertion", "/assertion", "/operator/projection/token.jwt", "/operator/../projection/assertion", "/operator/projection,/assertion", "/home/kit/projection/assertion", "/operator/\n/assertion"} {
		require.Error(t, validateKitLocalAssertionFile(path), path)
	}
	b := kitTestWorkloadBootstrap()
	b.AssertionFile = "/home/kit/workspace/assertion"
	require.Error(t, validateKitLocalBootstrap(b))
}

func TestKitLocalInspectRejectsMutableOrForeignProjection(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"Id": "id", "Image": "image", "Config": map[string]any{"User": "1000:1000"},
		"Mounts": []map[string]any{{"Type": "volume", "Name": "home", "Destination": "/home/kit"}, {"Type": "bind", "Source": "/operator/projection", "Destination": "/var/run/secrets/speakeasy/workload", "RW": false}},
	}
	encoded, err := json.Marshal(raw)
	require.NoError(t, err)
	info, err := decodeKitLocalInspect(encoded, 8081)
	require.NoError(t, err)
	require.Equal(t, "/operator/projection", info.AssertionDirectory)
	require.True(t, info.AssertionReadOnly)
	for _, mutation := range []func(map[string]any){
		func(m map[string]any) { m["RW"] = true },
		func(m map[string]any) { m["Type"] = "volume" },
	} {
		mounts, ok := raw["Mounts"].([]map[string]any)
		require.True(t, ok)
		mount := mounts[1]
		mount["RW"], mount["Type"] = false, "bind"
		mutation(mount)
		encoded, err = json.Marshal(raw)
		require.NoError(t, err)
		_, err = decodeKitLocalInspect(encoded, 8081)
		require.Error(t, err)
	}
}

func TestKitLocalBootstrapArchiveNeverCopiesAssertionsOrProviderCredentials(t *testing.T) {
	t.Parallel()
	b := kitTestWorkloadBootstrap()
	archive, err := kitLocalBootstrapArchive(b)
	require.NoError(t, err)
	reader := tar.NewReader(bytes.NewReader(archive))
	var files []string
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if header.Typeflag == tar.TypeDir {
			continue
		}
		files = append(files, header.Name)
		require.NotContains(t, header.Name, "assertion")
		require.NotContains(t, header.Name, "speakeasy.json")
	}
	require.ElementsMatch(t, []string{".kit/AGENTS.md", ".kit/config.toml", ".kit/credentials/mcp.json", ".kit/credentials/gateway-token"}, files)
}
