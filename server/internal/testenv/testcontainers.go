package testenv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
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

// UsePublishedPorts reports whether tests must dial published host ports to
// reach containers. Local rootful Linux can dial container IPs directly;
// Docker Desktop, rootless, and remote daemons require published ports.
// testcontainers publishes the ports either way (see [WithLoopbackPorts]).
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

// localDockerHost reports whether testcontainers reaches published ports on
// this machine: the Docker daemon is local (a unix socket, including Docker
// Desktop and rootless) and the tests are not running inside a container.
var localDockerHost = sync.OnceValue(func() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return false
	}
	defer func() { _ = provider.Close() }()

	host, err := provider.DaemonHost(ctx)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
})

var loopbackIPv4 = netip.MustParseAddr("127.0.0.1")

// WithLoopbackPorts binds the container's exposed ports to 127.0.0.1 when the
// Docker daemon is local. testcontainers publishes every exposed port, and
// Docker binds a published port on all interfaces by default, so a test
// container would otherwise be reachable from the network while tests run.
// Remote daemons
// keep the default binding because tests dial the daemon's host.
//
// The request must list the ports it uses. When it lists none, testcontainers
// publishes the image's ports, which this option cannot see.
//
// Apply it after any [testcontainers.WithHostConfigModifier], which replaces
// the modifier this option installs.
func WithLoopbackPorts() testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		if !localDockerHost() {
			return nil
		}

		// Installing a modifier skips testcontainers' default one, which only
		// copies deprecated request fields that these containers do not set.
		prev := req.HostConfigModifier
		req.HostConfigModifier = func(hostConfig *container.HostConfig) {
			if prev != nil {
				prev(hostConfig)
			}
			if hostConfig.PortBindings == nil {
				hostConfig.PortBindings = network.PortMap{}
			}
			// Bindings from an earlier modifier keep their host port.
			for _, bindings := range hostConfig.PortBindings {
				for i := range bindings {
					bindings[i].HostIP = loopbackIPv4
				}
			}
			// The request is read here rather than when the option runs, so
			// ports added by later options are bound too.
			for _, spec := range req.ExposedPorts {
				ports, err := network.ParsePortRange(spec)
				if err != nil {
					// testcontainers rejects the same spec before creating the
					// container.
					continue
				}
				for port := range ports.All() {
					if len(hostConfig.PortBindings[port]) == 0 {
						hostConfig.PortBindings[port] = []network.PortBinding{{HostIP: loopbackIPv4, HostPort: ""}}
					}
				}
			}
		}

		return nil
	}
}

// WithPublishedPortWait waits for Docker's host-port proxy only when tests dial
// published ports. Elsewhere tests dial container IPs, so containers keep a
// log or exec wait strategy for readiness.
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
