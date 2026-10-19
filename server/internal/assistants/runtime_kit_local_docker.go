package assistants

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"slices"
	"strconv"
)

// These extensions leave the legacy Run path unchanged. Kit must be created
// stopped so credentials can be copied before its gateway process ever starts.
func (d *dockerCLIEngine) CreateKit(ctx context.Context, spec kitLocalContainerSpec) (string, error) {
	args := kitLocalCreateArgs(d.guestPort, spec)
	id, err := d.exec(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("docker create kit gateway: %w", err)
	}
	return id, nil
}
func kitLocalCreateArgs(port int, spec kitLocalContainerSpec) []string {
	args := []string{"create", "--name", spec.Name, "--user", "1000:1000", "--workdir", "/home/kit", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--publish", "127.0.0.1::" + strconv.Itoa(port), "--add-host", LocalRuntimeHostGatewayAlias + ":host-gateway", "--volume", spec.VolumeName + ":/home/kit", "--env", "HOME=/home/kit"}
	if spec.ExtraCACertFile != "" {
		args = append(args, "--volume", spec.ExtraCACertFile+":"+localRuntimeCACertMountPath+":ro")
		// The image must honor standard TLS CA environment variables. There is no
		// legacy runner init script and no mutation of the image's system trust store.
		args = append(args, "--env", "SSL_CERT_FILE="+localRuntimeCACertMountPath, "--env", "NODE_EXTRA_CA_CERTS="+localRuntimeCACertMountPath)
	}
	for _, key := range slices.Sorted(maps.Keys(spec.Labels)) {
		args = append(args, "--label", key+"="+spec.Labels[key])
	}
	for _, key := range slices.Sorted(maps.Keys(spec.Env)) {
		if key != "HOME" {
			args = append(args, "--env", key+"="+spec.Env[key])
		}
	}
	args = append(args, "--entrypoint", spec.Command[0], spec.Image)
	args = append(args, spec.Command[1:]...)
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
	info := kitLocalContainerInfo{ID: obj.ID, ImageID: obj.Image, User: obj.Config.User, HomeVolume: "", Running: obj.State.Running, HostPort: 0, Labels: obj.Config.Labels}
	for _, mount := range obj.Mounts {
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

func (d *dockerCLIEngine) CopyKitBootstrap(ctx context.Context, id string, b KitLocalBootstrap) error {
	archive, err := kitLocalBootstrapArchive(b)
	if err != nil {
		return err
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
	}{{".kit/AGENTS.md", b.AgentsMD}, {".kit/config.toml", []byte(kitFileConfig)}, {".kit/credentials/" + KitInferenceCredentialFilename, b.InferenceJSON}, {".kit/credentials/mcp.json", b.MCPJSON}, {".kit/credentials/gateway-token", b.GatewayToken}}
	for _, name := range slices.Sorted(maps.Keys(b.MCPFiles)) {
		files = append(files, struct {
			name string
			data []byte
		}{".kit/credentials/" + name, b.MCPFiles[name]})
	}
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
