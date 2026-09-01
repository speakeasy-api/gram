package testenv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type testcontainersLogger struct {
	logger *slog.Logger
}

func (t *testcontainersLogger) Printf(format string, v ...any) {
	t.logger.Log(context.Background(), slog.LevelInfo, fmt.Sprintf(format, v...))
}

func NewTestcontainersLogger() log.Logger {
	return &testcontainersLogger{
		logger: slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
			RawLevel:    os.Getenv("LOG_LEVEL"),
			Pretty:      true,
			DataDogAttr: false,
		})),
	}
}

const dockerReadyTimeout = 30 * time.Second

var (
	dockerReady      atomic.Bool
	dockerReadyGroup singleflight.Group
)

func ensureDockerReady(ctx context.Context) error {
	if dockerReady.Load() {
		return nil
	}

	_, err, _ := dockerReadyGroup.Do("docker", func() (any, error) {
		if dockerReady.Load() {
			return nil, nil
		}

		readyCtx, cancel := context.WithTimeout(ctx, dockerReadyTimeout)
		defer cancel()

		cli, err := client.New(client.FromEnv)
		if err != nil {
			return nil, fmt.Errorf("create docker client: %w", err)
		}
		defer o11y.NoLogDefer(cli.Close)

		if err := retryDockerInfo(readyCtx, func(ctx context.Context) error {
			if _, err := cli.Info(ctx, client.InfoOptions{}); err != nil {
				return fmt.Errorf("query docker info: %w", err)
			}
			return nil
		}); err != nil {
			return nil, err
		}

		dockerReady.Store(true)
		return nil, nil
	})

	return err
}

func retryDockerInfo(ctx context.Context, info func(context.Context) error) error {
	for {
		err := info(ctx)
		if err == nil {
			return nil
		}

		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("docker info after retries: %w", errors.Join(err, ctx.Err()))
		case <-timer.C:
		}
	}
}

// UsePublishedPorts reports whether test containers must publish host ports
// for tests to reach them. Local rootful Linux can dial container IPs directly;
// Docker Desktop, rootless, and remote daemons require published ports.
var UsePublishedPorts = sync.OnceValue(func() bool {
	if runtime.GOOS != "linux" {
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		return true
	}
	defer func() { _ = cli.Close() }()

	if !strings.HasPrefix(cli.DaemonHost(), "unix://") {
		return true
	}

	res, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return true
	}

	return res.Info.OperatingSystem == "Docker Desktop" || slices.Contains(res.Info.SecurityOptions, "name=rootless")
})

// WithoutPublishedPorts strips module-declared exposed ports when tests can
// route directly to container IPs. Containers using this option must retain a
// log or exec wait strategy for unpublished-port readiness.
func WithoutPublishedPorts() testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		if !UsePublishedPorts() {
			req.ExposedPorts = nil
		}
		return nil
	}
}

// WithPublishedPortWait waits for Docker's host-port proxy only on platforms
// that publish container ports. Host-port waits cannot be used when ports are
// unpublished because testcontainers always resolves the mapped port first.
func WithPublishedPortWait(port nat.Port) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		if !UsePublishedPorts() {
			return nil
		}

		return testcontainers.WithAdditionalWaitStrategy(wait.ForListeningPort(string(port)))(req)
	}
}

// ContainerAddr returns the address tests should dial for a container port.
func ContainerAddr(ctx context.Context, container testcontainers.Container, port nat.Port) (string, error) {
	if !UsePublishedPorts() {
		ip, err := container.ContainerIP(ctx)
		if err != nil {
			return "", fmt.Errorf("get container ip: %w", err)
		}
		return net.JoinHostPort(ip, port.Port()), nil
	}

	host, err := container.Host(ctx)
	if err != nil {
		return "", fmt.Errorf("get container host: %w", err)
	}

	mapped, err := container.MappedPort(ctx, string(port))
	if err != nil {
		return "", fmt.Errorf("get mapped port for %s: %w", port, err)
	}

	if host == "localhost" {
		host = "127.0.0.1"
	}

	return net.JoinHostPort(host, mapped.Port()), nil
}
