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

// dockerCLIEngine implements containerEngine by shelling out to the docker
// CLI. It exists for local development only, so a contained CLI wrapper beats
// pulling the Docker SDK into the server module.
type dockerCLIEngine struct {
	guestPort int
}

func newDockerCLIEngine(guestPort int) *dockerCLIEngine {
	if guestPort == 0 {
		guestPort = defaultRuntimeGuestPort
	}
	return &dockerCLIEngine{guestPort: guestPort}
}

var _ containerEngine = (*dockerCLIEngine)(nil)

func (d *dockerCLIEngine) exec(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).Output() //nolint:gosec // fixed docker binary; args are engine-constructed, never raw user input
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("docker %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// isNotFoundOutput matches the docker CLI's not-found errors across object
// types and CLI versions ("No such container: ...", "no such volume", "No
// such image", "no such object").
func isNotFoundOutput(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such")
}

func (d *dockerCLIEngine) ImageID(ctx context.Context, imageRef string) (string, error) {
	out, err := d.exec(ctx, "image", "inspect", "--format", "{{.Id}}", imageRef)
	if isNotFoundOutput(err) {
		return "", errLocalImageNotFound
	}
	if err != nil {
		return "", err
	}
	return out, nil
}

// dockerContainerInspect is the subset of `docker container inspect` output
// the backend needs.
type dockerContainerInspect struct {
	ID    string `json:"Id"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	Image           string `json:"Image"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

func (d *dockerCLIEngine) Inspect(ctx context.Context, name string) (localContainerInfo, error) {
	out, err := d.exec(ctx, "container", "inspect", "--format", "{{json .}}", name)
	if isNotFoundOutput(err) {
		return localContainerInfo{}, errLocalContainerNotFound
	}
	if err != nil {
		return localContainerInfo{}, err
	}

	var inspect dockerContainerInspect
	if err := json.Unmarshal([]byte(out), &inspect); err != nil {
		return localContainerInfo{}, fmt.Errorf("decode docker inspect output for %s: %w", name, err)
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
		HostPort: hostPort,
	}, nil
}

func (d *dockerCLIEngine) Run(ctx context.Context, spec localContainerSpec) (string, error) {
	args := []string{
		"run", "--detach",
		"--name", spec.Name,
		// Publish the guest port to an ephemeral loopback port; the exact port
		// is re-resolved via Inspect after every start.
		"--publish", "127.0.0.1::" + strconv.Itoa(d.guestPort),
		// Docker Desktop resolves the alias natively; on native Linux engines
		// the host-gateway mapping provides the same name.
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

func (d *dockerCLIEngine) Start(ctx context.Context, name string) error {
	if _, err := d.exec(ctx, "start", name); err != nil {
		return err
	}
	return nil
}

func (d *dockerCLIEngine) Stop(ctx context.Context, name string) error {
	if _, err := d.exec(ctx, "stop", name); err != nil && !isNotFoundOutput(err) {
		return err
	}
	return nil
}

func (d *dockerCLIEngine) Remove(ctx context.Context, name string) error {
	if _, err := d.exec(ctx, "rm", "--force", name); err != nil && !isNotFoundOutput(err) {
		return err
	}
	return nil
}

func (d *dockerCLIEngine) RemoveVolume(ctx context.Context, name string) error {
	if _, err := d.exec(ctx, "volume", "rm", name); err != nil && !isNotFoundOutput(err) {
		return err
	}
	return nil
}
