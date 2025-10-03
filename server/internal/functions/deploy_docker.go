package functions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/hashicorp/go-cleanhttp"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type DockerRunner struct {
	logger      *slog.Logger
	client      *client.Client
	imgSelector ImageSelector
	encryption  *encryption.Client
}

func NewDockerRunner(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	imgSelector ImageSelector,
	encryption *encryption.Client,
) (*DockerRunner, error) {
	c, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
		client.WithTraceProvider(tracerProvider),
	)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	return &DockerRunner{
		logger:      logger,
		client:      c,
		imgSelector: imgSelector,
		encryption:  encryption,
	}, nil
}

func (dr *DockerRunner) onDeployError(ctx context.Context, logger *slog.Logger, containerID string, networkID string) {
	if containerID != "" {
		if rerr := dr.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); rerr != nil {
			logger.ErrorContext(
				ctx,
				"failed to remove container after deploy error",
				attr.SlogError(rerr),
				attr.SlogContainerID(containerID),
			)
		}
	}

	if networkID != "" {
		if rerr := dr.client.NetworkRemove(ctx, networkID); rerr != nil {
			logger.ErrorContext(
				ctx,
				"failed to remove network after deploy error",
				attr.SlogError(rerr),
				attr.SlogContainerNetworkID(networkID),
			)
		}
	}
}

func (dr *DockerRunner) prefixForContainers() string {
	return "gr-"
}

func (dr *DockerRunner) prefixForNetworks() string {
	return "gr-"
}

func (dr *DockerRunner) Deploy(ctx context.Context, req RunnerDeployRequest) (err error) {
	logger := dr.logger
	var c container.CreateResponse
	var n network.CreateResponse
	defer func() {
		if err != nil {
			dr.onDeployError(ctx, logger, c.ID, n.ID)
		}
	}()

	image, err := dr.imgSelector.Select(ctx, ImageRequest{
		ProjectID:    req.ProjectID,
		DeploymentID: req.DeploymentID,
		FunctionsID:  req.FunctionsID,
		Runtime:      req.Runtime,
	})
	if err != nil {
		return fmt.Errorf("select image for runtime: %s: %w", req.Runtime, err)
	}

	labels := map[string]string{
		"ai.getgram.app/workload":     "runner",
		"ai.getgram.app/project-id":   req.ProjectID.String(),
		"ai.getgram.app/functions-id": req.FunctionsID.String(),
	}

	var platform *v1.Platform = nil

	networkName := fmt.Sprintf("%s%s", dr.prefixForNetworks(), req.FunctionsID)
	n, err = dr.client.NetworkCreate(ctx, networkName, network.CreateOptions{
		Driver: "bridge",
		Labels: labels,
	})
	if err != nil {
		return fmt.Errorf("create runner docker network: %w", err)
	}

	networkingCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: {NetworkID: n.ID},
		},
	}

	containerCfg := &container.Config{
		Image:        image,
		Labels:       labels,
		ExposedPorts: nat.PortSet{"8888/tcp": {}},
	}
	hostCfg := &container.HostConfig{
		AutoRemove:    false,
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped", MaximumRetryCount: 3},
		PortBindings:  nat.PortMap{"8888/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "0"}}},
	}

	name := fmt.Sprintf("%s%s", dr.prefixForContainers(), req.FunctionsID)
	c, err = dr.client.ContainerCreate(ctx, containerCfg, hostCfg, networkingCfg, platform, name)
	if err != nil {
		return fmt.Errorf("create runner docker container: %w", err)
	}

	return nil
}

func (dr *DockerRunner) Update(ctx context.Context, req RunnerUpdateRequest) error {
	return oops.Permanent(errors.New("not implemented"))
}

func (dr *DockerRunner) Destroy(ctx context.Context, req RunnerDestroyRequest) (err error) {
	filters := container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("name", fmt.Sprintf("^%s", dr.prefixForContainers())),
			filters.Arg("label", "ai.getgram.app/workload=runner"),
			filters.Arg("label", fmt.Sprintf("ai.getgram.app/project-id=%s", req.ProjectID.String())),
			filters.Arg("label", fmt.Sprintf("ai.getgram.app/functions-id=%s", req.FunctionsID.String())),
		),
	}

	containers, lerr := dr.client.ContainerList(ctx, filters)
	if lerr != nil {
		err = errors.Join(err, fmt.Errorf("list containers: %w", lerr))
	}

	removeOpts := container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
		RemoveLinks:   true,
	}

	for _, c := range containers {
		if rmcErr := dr.client.ContainerRemove(ctx, c.ID, removeOpts); rmcErr != nil {
			err = errors.Join(err, fmt.Errorf("remove container: %s: %w", c.ID, rmcErr))
		}
	}

	networkName := fmt.Sprintf("gr-%s", req.FunctionsID)
	if rmnErr := dr.client.NetworkRemove(ctx, networkName); rmnErr != nil {
		err = errors.Join(err, fmt.Errorf("remove network: %s: %w", networkName, rmnErr))
	}

	if err != nil {
		return fmt.Errorf("destroy runner docker resources: %w", err)
	}

	return nil
}

func (dr *DockerRunner) CallTool(ctx context.Context, call RunnerToolCallRequest) (*http.Response, error) {
	containerName := fmt.Sprintf("gr-%s", call.FunctionsID)
	containerJSON, err := dr.client.ContainerInspect(ctx, containerName)
	if err != nil {
		return nil, fmt.Errorf("inspect container: %w", err)
	}

	portBindings := containerJSON.NetworkSettings.Ports["8888/tcp"]
	if len(portBindings) == 0 {
		return nil, fmt.Errorf("no port bindings found for container")
	}

	hostPort := portBindings[0].HostPort
	if hostPort == "" {
		return nil, fmt.Errorf("no host port found for container")
	}

	token, err := tokenV1(dr.encryption, tokenRequestV1{
		ID:  call.InvocationID.String(),
		Exp: time.Now().Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		return nil, fmt.Errorf("create tool call v1 bearer token: %w", err)
	}

	u := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%s", hostPort),
		Path:   "/tool-call",
	}

	payload := struct {
		Name        string            `json:"name"`
		Input       json.RawMessage   `json:"input"`
		Environment map[string]string `json:"environment,omitempty,omitzero"`
	}{
		Name:        call.Name,
		Input:       call.Input,
		Environment: call.Environment,
	}

	j, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal tool call payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewBuffer(j))
	if err != nil {
		return nil, fmt.Errorf("create tool call request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return req.Body.Close() })
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gram-server")

	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: otelhttp.NewTransport(
			cleanhttp.DefaultTransport(),
			otelhttp.WithPropagators(propagation.TraceContext{}),
		),
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call tool: %w", err)
	}

	return resp, nil
}

func (dr *DockerRunner) Close(context.Context) error {
	if err := dr.client.Close(); err != nil {
		return fmt.Errorf("close docker client: %w", err)
	}
	return nil
}
