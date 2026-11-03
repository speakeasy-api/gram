package assistants

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Kit's native roots honor SSL_CERT_FILE as a replacement, not an addition.
// Build a private bundle as UID 1000 without changing system trust. Paths and
// the gateway command are positional arguments, never shell source.
const kitLocalCABundleScript = `set -eu
umask 077
bundle="${TMPDIR:-/tmp}/kit-ca-bundle.pem"
rm -f "$bundle"
set -C
trap 'rm -f "$bundle"' EXIT
cat "$1" > "$bundle"
set +C
printf '\n' >> "$bundle"
cat "$2" >> "$bundle"
shift 2
export SSL_CERT_FILE="$bundle"
exec "$@"
`

// These extensions leave the legacy Run path unchanged. Kit must be created
// stopped so credentials can be copied before its gateway process ever starts.
func (d *dockerCLIEngine) CreateKit(ctx context.Context, spec kitLocalContainerSpec) (string, error) {
	if err := validateKitLocalAssertionFile(spec.WorkloadAssertionFile); err != nil {
		return "", err
	}
	if err := validateKitLocalProjectionDirectory(spec.WorkloadAssertionFile); err != nil {
		return "", err
	}
	if err := checkKitLocalProjectionAccess(ctx, spec, d.exec); err != nil {
		return "", err
	}
	args := kitLocalCreateArgs(d.guestPort, spec)
	id, err := d.exec(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("docker create kit gateway: %w", err)
	}
	return id, nil
}

// Probe through the actual daemon mapping instead of assuming host UID 1000
// means guest UID 1000 (rootless engines usually map the host owner to root).
// No assertion bytes are read, copied, printed, or stored in a runtime volume.
func checkKitLocalProjectionAccess(ctx context.Context, spec kitLocalContainerSpec, run func(context.Context, ...string) (string, error)) error {
	args := []string{"run", "--rm", "--network", "none", "--read-only", "--user", "1000:1000", "--workdir", "/", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--mount", "type=bind,src=" + filepath.Dir(spec.WorkloadAssertionFile) + ",dst=" + filepath.Dir(KitWorkloadAssertionPath) + ",readonly",
		"--entrypoint", "/bin/sh", spec.Image, "-c", `test -f "$1" && test -r "$1" && : < "$1"`, "kit-projection-check", KitWorkloadAssertionPath}
	if _, err := run(ctx, args...); err != nil {
		exit, hasExit := errors.AsType[interface {
			error
			ExitCode() int
		}](err)
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !hasExit || (exit.ExitCode() != 1 && exit.ExitCode() != 2) {
			return fmt.Errorf("run kit assertion projection access probe: %w", err)
		}
		return fmt.Errorf("kit assertion projection must be readable by guest UID 1000 through the configured container daemon: provision the directory and each rotated assertion with ownership mapped to guest UID 1000, retaining 0700/0600 permissions; rootless host-owner-to-root mapping alone is unsupported (do not run Kit as root or widen permissions): %w", err)
	}
	return nil
}

func kitLocalCreateArgs(port int, spec kitLocalContainerSpec) []string {
	args := []string{"create", "--name", spec.Name, "--user", "1000:1000", "--workdir", "/home/kit", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--publish", "127.0.0.1::" + strconv.Itoa(port), "--add-host", LocalRuntimeHostGatewayAlias + ":host-gateway", "--volume", spec.VolumeName + ":/home/kit", "--env", "HOME=/home/kit"}
	// Bind the directory, not the assertion inode, so atomic file replacement
	// remains visible. The mounted projection is outside persistent HOME.
	args = append(args, "--mount", "type=bind,src="+filepath.Dir(spec.WorkloadAssertionFile)+",dst="+filepath.Dir(KitWorkloadAssertionPath)+",readonly")
	if spec.Network != "" {
		args = append(args, "--network", spec.Network)
	}
	if spec.ExtraCACertFile != "" {
		args = append(args, "--volume", spec.ExtraCACertFile+":"+localRuntimeCACertMountPath+":ro")
		args = append(args, "--env", "NODE_EXTRA_CA_CERTS="+localRuntimeCACertMountPath)
	}
	for _, key := range slices.Sorted(maps.Keys(spec.Labels)) {
		args = append(args, "--label", key+"="+spec.Labels[key])
	}
	for _, key := range slices.Sorted(maps.Keys(spec.Env)) {
		if key != "HOME" {
			args = append(args, "--env", key+"="+spec.Env[key])
		}
	}
	if spec.ExtraCACertFile != "" {
		args = append(args, "--entrypoint", "/bin/sh", spec.Image, "-c", kitLocalCABundleScript, "kit-ca-bundle", "/etc/ssl/certs/ca-certificates.crt", localRuntimeCACertMountPath)
		args = append(args, spec.Command...)
	} else {
		args = append(args, "--entrypoint", spec.Command[0], spec.Image)
		args = append(args, spec.Command[1:]...)
	}
	args = append(args, spec.Args...)
	return args
}

func (d *dockerCLIEngine) InspectKit(ctx context.Context, name string) (kitLocalContainerInfo, error) {
	out, err := d.exec(ctx, "container", "inspect", "--format", "{{json .}}", name)
	if isNotFoundOutput(err) {
		return kitLocalContainerInfo{}, errLocalContainerNotFound
	}
	if err != nil {
		return kitLocalContainerInfo{}, fmt.Errorf("inspect kit container: %w", err)
	}
	return decodeKitLocalInspect([]byte(out), d.guestPort)
}
func decodeKitLocalInspect(raw []byte, port int) (kitLocalContainerInfo, error) {
	var obj struct {
		ID     string `json:"Id"`
		Image  string `json:"Image"`
		Config struct {
			Labels map[string]string `json:"Labels"`
			User   string            `json:"User"`
		} `json:"Config"`
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
		Mounts []struct {
			Type        string `json:"Type"`
			Name        string `json:"Name"`
			Source      string `json:"Source"`
			RW          bool   `json:"RW"`
			Destination string `json:"Destination"`
		} `json:"Mounts"`
		NetworkSettings struct {
			Ports map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return kitLocalContainerInfo{}, fmt.Errorf("decode kit inspect: %w", err)
	}
	info := kitLocalContainerInfo{AssertionDirectory: "", AssertionReadOnly: false, ID: obj.ID, ImageID: obj.Image, User: obj.Config.User, HomeVolume: "", Running: obj.State.Running, HostPort: 0, Labels: obj.Config.Labels}
	for _, mount := range obj.Mounts {
		if mount.Destination == filepath.Dir(KitWorkloadAssertionPath) {
			if mount.Type != "bind" || mount.RW || info.AssertionDirectory != "" {
				return kitLocalContainerInfo{}, fmt.Errorf("kit assertion mount must be one read-only bind directory")
			}
			info.AssertionDirectory, info.AssertionReadOnly = mount.Source, true
		}
		if mount.Destination == "/home/kit" && mount.Type == "volume" {
			info.HomeVolume = mount.Name
		}
	}
	for _, binding := range obj.NetworkSettings.Ports[strconv.Itoa(port)+"/tcp"] {
		if binding.HostIP != "127.0.0.1" {
			return kitLocalContainerInfo{}, fmt.Errorf("kit local gateway must only publish to IPv4 loopback")
		}
		n, err := strconv.Atoi(binding.HostPort)
		if err != nil || n < 1 || n > 65535 {
			return kitLocalContainerInfo{}, fmt.Errorf("kit local gateway has an invalid published port")
		}
		info.HostPort = n
	}
	return info, nil
}
func (d *dockerCLIEngine) InspectKitVolume(ctx context.Context, name string) (map[string]string, error) {
	out, err := d.exec(ctx, "volume", "inspect", "--format", "{{json .Labels}}", name)
	if isNotFoundOutput(err) {
		return nil, errLocalContainerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("inspect kit HOME volume: %w", err)
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(out), &labels); err != nil {
		return nil, fmt.Errorf("decode kit HOME labels: %w", err)
	}
	return labels, nil
}
func (d *dockerCLIEngine) CreateKitVolume(ctx context.Context, name string, labels map[string]string) error {
	args := []string{"volume", "create"}
	for _, key := range slices.Sorted(maps.Keys(labels)) {
		args = append(args, "--label", key+"="+labels[key])
	}
	args = append(args, name)
	if _, err := d.exec(ctx, args...); err != nil {
		return fmt.Errorf("create kit HOME volume: %w", err)
	}
	return nil
}

// The stopped HOME volume is the only writable mount. Replacing this directory
// removes credentials omitted by the new bootstrap, including dangling symlinks.
//
//nolint:gosec // G101: fixed filesystem cleanup commands, not a credential or embedded secret.
const kitLocalCredentialResetScript = `set -eu
umask 077
test ! -L "$1/.kit"
mkdir -p "$1/.kit"
rm -rf "$1/.kit/credentials"
mkdir -m 700 "$1/.kit/credentials"
`

func kitLocalCredentialResetArgs(info kitLocalContainerInfo) ([]string, error) {
	if info.Running || info.ID == "" || info.ImageID == "" || info.HomeVolume == "" {
		return nil, fmt.Errorf("credential replacement requires a stopped Kit HOME volume")
	}
	return []string{"run", "--rm", "--network", "none", "--read-only", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--mount", "type=volume,src=" + info.HomeVolume + ",dst=/kit-home", "--entrypoint", "/bin/sh", info.ImageID, "-c", kitLocalCredentialResetScript, "kit-credentials-reset", "/kit-home"}, nil
}

func (d *dockerCLIEngine) CopyKitBootstrap(ctx context.Context, id string, b KitLocalBootstrap) error {
	archive, err := kitLocalBootstrapArchive(b)
	if err != nil {
		return err
	}
	info, err := d.InspectKit(ctx, id)
	if err != nil {
		return err
	}
	if info.ID != id {
		return fmt.Errorf("credential replacement container identity changed")
	}
	args, err := kitLocalCredentialResetArgs(info)
	if err != nil {
		return err
	}
	if _, err := d.exec(ctx, args...); err != nil {
		return fmt.Errorf("replace kit credentials directory: %w", err)
	}
	// -a preserves UID/GID from fixed archive headers. No secret is placed in
	// argv, environment, a host file, logs, or returned Docker stderr.
	cmd := exec.CommandContext(ctx, "docker", "cp", "-a", "-", id+":/home/kit") //nolint:gosec // Fixed Docker operation and engine-resolved container ID; credentials are stdin only.
	cmd.Stdin = bytes.NewReader(archive)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("copy kit bootstrap archive: %w", err)
	}
	return nil
}

// Fixed names avoid archive traversal. The image owns HOME; this archive creates
// only nonroot private Kit directories and four caller-supplied files.
func kitLocalBootstrapArchive(b KitLocalBootstrap) ([]byte, error) {
	if err := validateKitLocalBootstrap(b); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, name := range []string{".kit/", ".kit/credentials/", "workspace/"} {
		header := new(tar.Header)
		header.Name = name
		header.Mode = 0700
		header.Uid = 1000
		header.Gid = 1000
		header.Typeflag = tar.TypeDir
		if err := writer.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("write kit directory archive header: %w", err)
		}
	}
	files := []struct {
		name string
		data []byte
	}{{".kit/AGENTS.md", b.AgentsMD}, {".kit/config.toml", []byte(kitBootstrapConfig(b))}, {".kit/credentials/mcp.json", b.MCPJSON}, {".kit/credentials/gateway-token", b.GatewayToken}}

	for _, file := range files {
		header := new(tar.Header)
		header.Name = file.name
		header.Mode = 0600
		header.Uid = 1000
		header.Gid = 1000
		header.Typeflag = tar.TypeReg
		header.Size = int64(len(file.data))
		if err := writer.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("write kit file archive header: %w", err)
		}
		if _, err := writer.Write(file.data); err != nil {
			return nil, fmt.Errorf("write kit bootstrap archive: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close kit bootstrap archive: %w", err)
	}
	return buffer.Bytes(), nil
}

// Local operators project a file named assertion in a dedicated directory.
// Mounting its parent preserves atomic rotation without copying token bytes.
func validateKitLocalAssertionFile(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "assertion" || filepath.Dir(path) == "/" || strings.ContainsAny(path, ",\r\n\x00") || strings.HasPrefix(path, "/home/kit/") {
		return fmt.Errorf("kit local requires an absolute clean projection path named assertion in a dedicated directory outside persistent HOME")
	}
	return nil
}

// A directory mount must not grant access to unrelated host files. Operators
// must keep this directory dedicated to assertion projection for its lifetime.
func validateKitLocalProjectionDirectory(assertion string) error {
	directory := filepath.Dir(assertion)
	parent, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect kit assertion projection directory: %w", err)
	}
	if !parent.IsDir() {
		return fmt.Errorf("kit assertion projection parent must be a directory, not a file or symlink")
	}
	if parent.Mode().Perm() != 0o700 {
		return fmt.Errorf("kit assertion projection directory must have permissions 0700")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read kit assertion projection directory: %w", err)
	}
	if len(entries) != 1 || entries[0].Name() != "assertion" {
		return fmt.Errorf("kit assertion projection requires a dedicated directory containing only assertion")
	}
	entry, err := entries[0].Info()
	if err != nil {
		return fmt.Errorf("inspect projected kit assertion: %w", err)
	}
	if !entry.Mode().IsRegular() {
		return fmt.Errorf("projected kit assertion must be a regular file")
	}
	if entry.Mode().Perm() != 0o600 {
		return fmt.Errorf("projected kit assertion must have permissions 0600")
	}
	return nil
}
