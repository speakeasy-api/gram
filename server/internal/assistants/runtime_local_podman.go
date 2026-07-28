package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// podmanCLIEngine implements containerEngine by shelling out to the podman
// CLI. It exists for local development only, so a contained CLI wrapper beats
// pulling a container-engine SDK into the server module.
type podmanCLIEngine struct {
	guestPort int
}

func newPodmanCLIEngine(guestPort int) *podmanCLIEngine {
	if guestPort == 0 {
		guestPort = defaultRuntimeGuestPort
	}
	return &podmanCLIEngine{guestPort: guestPort}
}

var _ containerEngine = (*podmanCLIEngine)(nil)

func (d *podmanCLIEngine) exec(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "podman", args...).Output() //nolint:gosec // fixed podman binary; args are engine-constructed, never raw user input
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("podman %s: %w: %s", args[0], err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("podman %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// isNotFoundOutput matches the podman CLI's not-found errors across object
// types and CLI versions ("no such container: ...", "no such volume", "no
// such object", and image lookups which surface as "... image not known").
func isNotFoundOutput(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such") || strings.Contains(msg, "not known")
}

// podmanContainerInspect is the subset of `podman container inspect` output
// the backend needs. Podman's native inspect keeps Docker-compatible key
// casing for all of these fields.
type podmanContainerInspect struct {
	ID    string `json:"Id"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	Image  string `json:"Image"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

func (d *podmanCLIEngine) ImageID(ctx context.Context, imageRef string) (string, error) {
	out, err := d.exec(ctx, "image", "inspect", "--format", "{{.Id}}", imageRef)
	if isNotFoundOutput(err) {
		return "", errLocalImageNotFound
	}
	if err != nil {
		return "", err
	}
	return out, nil
}

func (d *podmanCLIEngine) Inspect(ctx context.Context, name string) (localContainerInfo, error) {
	out, err := d.exec(ctx, "container", "inspect", "--format", "{{json .}}", name)
	if isNotFoundOutput(err) {
		return localContainerInfo{}, errLocalContainerNotFound
	}
	if err != nil {
		return localContainerInfo{}, err
	}

	var inspect podmanContainerInspect
	if err := json.Unmarshal([]byte(out), &inspect); err != nil {
		return localContainerInfo{}, fmt.Errorf("decode podman inspect output for %s: %w", name, err)
	}

	hostPort := 0
	for _, binding := range inspect.NetworkSettings.Ports[strconv.Itoa(d.guestPort)+"/tcp"] {
		port, err := strconv.Atoi(binding.HostPort)
		if err != nil {
			continue
		}
		hostPort = port
		break
	}

	return localContainerInfo{
		ID:       inspect.ID,
		Running:  inspect.State.Running,
		ImageID:  inspect.Image,
		SpecHash: inspect.Config.Labels[runtimeLabelSpecHash],
		HostPort: hostPort,
	}, nil
}

func (d *podmanCLIEngine) Run(ctx context.Context, spec localContainerSpec) (string, error) {
	args := []string{
		"run", "--detach",
		"--name", spec.Name,
		// Publish the guest port to an ephemeral loopback port; the exact port
		// is re-resolved via Inspect after every start.
		"--publish", "127.0.0.1::" + strconv.Itoa(d.guestPort),
		// The host-gateway special value (podman >= 5.3) maps the alias to an
		// address that routes back to the host under both rootless (pasta) and
		// rootful networking.
		"--add-host", LocalRuntimeHostGatewayAlias + ":host-gateway",
		"--volume", spec.VolumeName + ":" + localRuntimeWorkdirMountPath,
	}
	if spec.ExtraCACertFile != "" {
		args = append(args, "--volume", spec.ExtraCACertFile+":"+localRuntimeCACertMountPath+":ro")
	}
	for _, key := range slices.Sorted(maps.Keys(spec.Labels)) {
		args = append(args, "--label", key+"="+spec.Labels[key])
	}
	for _, key := range slices.Sorted(maps.Keys(spec.Env)) {
		args = append(args, "--env", key+"="+spec.Env[key])
	}
	args = append(args, spec.Image)

	out, err := d.exec(ctx, args...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already in use") {
			return "", errLocalContainerNameInUse
		}
		return "", err
	}
	return out, nil
}

func (d *podmanCLIEngine) Start(ctx context.Context, name string) error {
	if _, err := d.exec(ctx, "start", name); err != nil {
		return err
	}
	return nil
}

func (d *podmanCLIEngine) Stop(ctx context.Context, name string) error {
	if _, err := d.exec(ctx, "stop", name); err != nil && !isNotFoundOutput(err) {
		return err
	}
	return nil
}

func (d *podmanCLIEngine) Remove(ctx context.Context, name string) error {
	if _, err := d.exec(ctx, "rm", "--force", name); err != nil && !isNotFoundOutput(err) {
		return err
	}
	return nil
}

func (d *podmanCLIEngine) RemoveVolume(ctx context.Context, name string) error {
	if _, err := d.exec(ctx, "volume", "rm", name); err != nil && !isNotFoundOutput(err) {
		return err
	}
	return nil
}
