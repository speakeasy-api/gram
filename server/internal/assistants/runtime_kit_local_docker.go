package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Kit's native roots honor SSL_CERT_FILE as a replacement, not an addition.
// Build a private bundle as UID 1000 without changing system trust. Paths and
// the init/serve command are positional arguments, never shell source.
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
export REQUESTS_CA_BUNDLE="$bundle"
export CURL_CA_BUNDLE="$bundle"
export GIT_SSL_CAINFO="$bundle"
export PIP_CERT="$bundle"
export NODE_EXTRA_CA_CERTS="$bundle"
export DENO_CERT="$bundle"
exec "$@"
`

// kitLocalInitCommand returns the harness-agnostic kit-ate-init entrypoint and
// native Kit ACP serve command matching agents/ate-runtime/Dockerfile.
func kitLocalInitCommand(initPort, acpPort int) []string {
	return []string{
		"/usr/local/bin/kit-ate-init",
		"--root=/home/kit",
		"--listen=:" + strconv.Itoa(initPort),
		"--probe-addr=127.0.0.1:" + strconv.Itoa(acpPort),
		"--",
		"/usr/local/bin/kit", "serve",
		"--root", KitWorkspacePath,
		"--remote-acp",
		"--no-a2a",
		"--no-stdio",
		"--a2a", "0.0.0.0:" + strconv.Itoa(acpPort),
		"--server-credential-file", "/home/kit/.kit/credentials/gateway-token",
	}
}

type kitLocalDockerEngine struct {
	*dockerCLIEngine
	initPort int
	acpPort  int
}

func newKitLocalDockerEngine(initPort, acpPort int) *kitLocalDockerEngine {
	return &kitLocalDockerEngine{
		dockerCLIEngine: newDockerCLIEngine(acpPort),
		initPort:        initPort,
		acpPort:         acpPort,
	}
}

// CreateKit creates a stopped per-thread container running kit-ate-init + kit serve --remote-acp.
func (d *kitLocalDockerEngine) CreateKit(ctx context.Context, spec kitLocalContainerSpec) (string, error) {
	if err := validateKitLocalAssertionFile(spec.WorkloadAssertionFile); err != nil {
		return "", err
	}
	if err := validateKitLocalProjectionDirectory(spec.WorkloadAssertionFile); err != nil {
		return "", err
	}
	if err := checkKitLocalProjectionAccess(ctx, spec, d.exec); err != nil {
		return "", err
	}
	args := kitLocalCreateArgs(d.initPort, d.acpPort, spec)
	id, err := d.exec(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("docker create kit thread container: %w", err)
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

func kitLocalCreateArgs(initPort, acpPort int, spec kitLocalContainerSpec) []string {
	args := []string{
		"create",
		"--name", spec.Name,
		"--user", "1000:1000",
		"--workdir", "/home/kit",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--publish", "127.0.0.1::" + strconv.Itoa(initPort),
		"--publish", "127.0.0.1::" + strconv.Itoa(acpPort),
		"--add-host", LocalRuntimeHostGatewayAlias + ":host-gateway",
		"--volume", spec.VolumeName + ":/home/kit",
		"--env", "HOME=/home/kit",
	}
	// Bind the directory, not the assertion inode, so atomic file replacement
	// remains visible. The mounted projection is outside persistent HOME.
	args = append(args, "--mount", "type=bind,src="+filepath.Dir(spec.WorkloadAssertionFile)+",dst="+filepath.Dir(KitWorkloadAssertionPath)+",readonly")
	if spec.Network != "" {
		args = append(args, "--network", spec.Network)
	}
	if spec.ExtraCACertFile != "" {
		args = append(args, "--volume", spec.ExtraCACertFile+":"+localRuntimeCACertMountPath+":ro")
	}
	for _, key := range slices.Sorted(maps.Keys(spec.Labels)) {
		args = append(args, "--label", key+"="+spec.Labels[key])
	}
	for _, key := range slices.Sorted(maps.Keys(spec.Env)) {
		if key != "HOME" {
			args = append(args, "--env", key+"="+spec.Env[key])
		}
	}
	initCmd := kitLocalInitCommand(initPort, acpPort)
	if spec.ExtraCACertFile != "" {
		args = append(args, "--entrypoint", "/bin/sh", spec.Image, "-c", kitLocalCABundleScript, "kit-ca-bundle", "/etc/ssl/certs/ca-certificates.crt", localRuntimeCACertMountPath)
		args = append(args, initCmd...)
	} else {
		args = append(args, "--entrypoint", initCmd[0], spec.Image)
		args = append(args, initCmd[1:]...)
	}
	return args
}

func (d *kitLocalDockerEngine) InspectKit(ctx context.Context, name string) (kitLocalContainerInfo, error) {
	out, err := d.exec(ctx, "container", "inspect", "--format", "{{json .}}", name)
	if isNotFoundOutput(err) {
		return kitLocalContainerInfo{}, errLocalContainerNotFound
	}
	if err != nil {
		return kitLocalContainerInfo{}, fmt.Errorf("inspect kit container: %w", err)
	}
	return decodeKitLocalInspect([]byte(out), d.initPort, d.acpPort)
}

func decodeKitLocalInspect(raw []byte, initPort, acpPort int) (kitLocalContainerInfo, error) {
	var obj struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
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
	info := kitLocalContainerInfo{
		AssertionDirectory: "",
		AssertionReadOnly:  false,
		ID:                 obj.ID,
		Name:               strings.TrimPrefix(obj.Name, "/"),
		ImageID:            obj.Image,
		User:               obj.Config.User,
		HomeVolume:         "",
		Running:            obj.State.Running,
		InitHostPort:       0,
		HostPort:           0,
		Labels:             obj.Config.Labels,
	}
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
	parseLoopbackPort := func(containerPort int) (int, error) {
		var published int
		for _, binding := range obj.NetworkSettings.Ports[strconv.Itoa(containerPort)+"/tcp"] {
			if binding.HostIP != "127.0.0.1" {
				return 0, fmt.Errorf("kit local container must only publish to IPv4 loopback")
			}
			n, err := strconv.Atoi(binding.HostPort)
			if err != nil || n < 1 || n > 65535 {
				return 0, fmt.Errorf("kit local container has an invalid published port")
			}
			published = n
		}
		return published, nil
	}
	var err error
	info.InitHostPort, err = parseLoopbackPort(initPort)
	if err != nil {
		return kitLocalContainerInfo{}, err
	}
	info.HostPort, err = parseLoopbackPort(acpPort)
	if err != nil {
		return kitLocalContainerInfo{}, err
	}
	return info, nil
}

func (d *kitLocalDockerEngine) ListKitContainers(ctx context.Context, runtimeID string) ([]string, error) {
	out, err := d.exec(ctx, "ps", "-a", "--filter", "label=gram.ai/runtime-lineage="+runtimeID, "--format", "{{.Names}}")
	if err != nil {
		return nil, fmt.Errorf("list kit local containers: %w", err)
	}
	return splitNonEmptyLines(out), nil
}

func (d *kitLocalDockerEngine) ListKitVolumes(ctx context.Context, runtimeID string) ([]string, error) {
	out, err := d.exec(ctx, "volume", "ls", "--filter", "label=gram.ai/runtime-lineage="+runtimeID, "--format", "{{.Name}}")
	if err != nil {
		return nil, fmt.Errorf("list kit local volumes: %w", err)
	}
	return splitNonEmptyLines(out), nil
}

func splitNonEmptyLines(raw string) []string {
	var out []string
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (d *kitLocalDockerEngine) InspectKitVolume(ctx context.Context, name string) (map[string]string, error) {
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

func (d *kitLocalDockerEngine) CreateKitVolume(ctx context.Context, name string, labels map[string]string) error {
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
