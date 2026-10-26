package assistants

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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

func TestKitLocalProjectionRequiresDedicatedDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, 0700))
	assertion := filepath.Join(directory, "assertion")
	require.NoError(t, os.WriteFile(assertion, []byte("test-assertion"), 0600))
	require.NoError(t, validateKitLocalProjectionDirectory(assertion))
	sibling := filepath.Join(directory, "unrelated-host-file")
	require.NoError(t, os.WriteFile(sibling, []byte("not-for-runtime"), 0600))
	require.ErrorContains(t, validateKitLocalProjectionDirectory(assertion), "dedicated directory")
	require.NoError(t, os.Remove(sibling))
	// Atomic assertion replacement remains compatible with the dedicated mount.
	next := filepath.Join(directory, "next")
	require.NoError(t, os.WriteFile(next, []byte("rotated-test-assertion"), 0600))
	require.NoError(t, os.Rename(next, assertion))
	require.NoError(t, validateKitLocalProjectionDirectory(assertion))
	require.NoError(t, os.Remove(assertion))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "other"), assertion))
	require.ErrorContains(t, validateKitLocalProjectionDirectory(assertion), "regular file")
}

func TestKitLocalProjectionPreservesFilesystemErrors(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	require.ErrorIs(t, validateKitLocalProjectionDirectory(filepath.Join(directory, "missing", "assertion")), os.ErrNotExist)
	file := filepath.Join(directory, "not-a-directory")
	require.NoError(t, os.WriteFile(file, []byte("test"), 0600))
	require.ErrorContains(t, validateKitLocalProjectionDirectory(filepath.Join(file, "assertion")), "must be a directory")
	linked := filepath.Join(directory, "linked-directory")
	require.NoError(t, os.Symlink(t.TempDir(), linked))
	require.ErrorContains(t, validateKitLocalProjectionDirectory(filepath.Join(linked, "assertion")), "not a file or symlink")
}

func TestKitLocalProjectionRequiresPrivatePermissions(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	assertion := filepath.Join(directory, "assertion")
	require.NoError(t, os.WriteFile(assertion, []byte("test-assertion"), 0600))
	require.NoError(t, os.Chmod(directory, 0750))
	require.ErrorContains(t, validateKitLocalProjectionDirectory(assertion), "0700")
	require.NoError(t, os.Chmod(directory, 0700))
	require.NoError(t, os.Chmod(assertion, 0640))
	require.ErrorContains(t, validateKitLocalProjectionDirectory(assertion), "0600")
	require.NoError(t, os.Chmod(assertion, 0600))
	require.NoError(t, validateKitLocalProjectionDirectory(assertion))
}

func TestKitLocalProjectionAccessUsesDaemonMapping(t *testing.T) {
	t.Parallel()
	for _, readable := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmapped-owner", true: "mapped-owner"}[readable], func(t *testing.T) {
			t.Parallel()
			spec := kitLocalContainerSpec{WorkloadAssertionFile: "/operator/projection/assertion", Image: "kit:v0.2.19"}
			denied := fmt.Errorf("docker run: %w", kitProjectionExitError(1))
			called := false
			err := checkKitLocalProjectionAccess(t.Context(), spec, func(_ context.Context, args ...string) (string, error) {
				called = true
				require.Equal(t, []string{"run", "--rm", "--network", "none", "--read-only", "--user", "1000:1000", "--workdir", "/", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
					"--mount", "type=bind,src=/operator/projection,dst=/var/run/secrets/speakeasy/workload,readonly",
					"--entrypoint", "/bin/sh", spec.Image, "-c", `test -f "$1" && test -r "$1" && : < "$1"`, "kit-projection-check", KitWorkloadAssertionPath}, args)
				if !readable {
					return "", denied
				}
				return "", nil
			})
			require.True(t, called)
			if readable {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, denied)
				require.ErrorContains(t, err, "ownership mapped to guest UID 1000")
				require.ErrorContains(t, err, "rootless host-owner-to-root mapping alone is unsupported")
			}
		})
	}
}

type kitProjectionExitError int

func (e kitProjectionExitError) Error() string { return fmt.Sprintf("exit status %d", e) }
func (e kitProjectionExitError) ExitCode() int { return int(e) }

func TestKitLocalProjectionAccessClassifiesFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		cause     error
		cancel    bool
		ownership bool
	}{
		{name: "daemon", cause: errors.New("cannot connect to daemon"), cancel: false, ownership: false},
		{name: "canceled", cause: context.Canceled, cancel: false, ownership: false},
		{name: "deadline", cause: context.DeadlineExceeded, cancel: false, ownership: false},
		{name: "canceled-process", cause: kitProjectionExitError(1), cancel: true, ownership: false},
		{name: "unreadable", cause: kitProjectionExitError(1), cancel: false, ownership: true},
		{name: "open-denied", cause: kitProjectionExitError(2), cancel: false, ownership: true},
		{name: "mount", cause: kitProjectionExitError(125), cancel: false, ownership: false},
		{name: "cannot-execute", cause: kitProjectionExitError(126), cancel: false, ownership: false},
		{name: "missing-shell", cause: kitProjectionExitError(127), cancel: false, ownership: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			wrapped := fmt.Errorf("docker run: %w", tc.cause)
			err := checkKitLocalProjectionAccess(ctx, kitLocalContainerSpec{WorkloadAssertionFile: "/operator/projection/assertion", Image: "kit:v0.2.19"}, func(context.Context, ...string) (string, error) { return "", wrapped })
			require.ErrorIs(t, err, tc.cause)
			if tc.ownership {
				require.ErrorContains(t, err, "ownership mapped to guest UID 1000")
			} else {
				require.ErrorContains(t, err, "run kit assertion projection access probe")
				require.NotContains(t, err.Error(), "ownership mapped")
			}
		})
	}
}
