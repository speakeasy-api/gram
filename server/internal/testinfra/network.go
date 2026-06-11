package testinfra

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// UsePublishedPorts reports whether test containers must publish host ports
// for tests to reach them. Published ports are the source of two recurring CI
// flake classes: Docker's dynamic host-port allocator can pick a port already
// held by a host process (container start fails with "address already in
// use"), and dials through the publishing path (NAT rules, userland proxy)
// can be misrouted to unrelated host listeners under heavy parallel container
// churn. When the test process can route to container IPs directly — a local,
// rootful Docker daemon on Linux — ports are not published at all and tests
// dial <container IP>:<container port>. Docker Desktop and rootless daemons
// keep containers behind a VM or user namespace where container IPs are
// unreachable, so they retain published ports.
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

	// A remote daemon (tcp:// or ssh:// DOCKER_HOST) runs containers on
	// another machine, where container IPs are not routable from here.
	if !strings.HasPrefix(cli.DaemonHost(), "unix://") {
		return true
	}

	res, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return true
	}

	return res.Info.OperatingSystem == "Docker Desktop" || slices.Contains(res.Info.SecurityOptions, "name=rootless")
})

// WithoutPublishedPorts strips the exposed ports declared by a container
// module so Docker never allocates host ports for them. No-op when
// UsePublishedPorts reports true.
func WithoutPublishedPorts() testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		if !UsePublishedPorts() {
			req.ExposedPorts = nil
		}
		return nil
	}
}

// PortWait waits for a container port to accept connections, checking only
// from inside the container when the port is not published on the host.
func PortWait(port nat.Port) wait.Strategy {
	w := wait.ForListeningPort(string(port))
	if !UsePublishedPorts() {
		return w.SkipExternalCheck()
	}
	return w
}

// ContainerAddr returns the address tests should dial to reach a container
// port: the container IP itself when ports are unpublished, otherwise the
// published endpoint on the Docker host.
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

	// Avoid a DNS lookup for localhost inside synctest bubbles without
	// changing arbitrary Docker/Testcontainers endpoints. Re-resolving the
	// host here can pick an address that is not the actual published
	// endpoint (for example ::1 instead of Docker's IPv4 localhost binding).
	if host == "localhost" {
		host = "127.0.0.1"
	}

	return net.JoinHostPort(host, mapped.Port()), nil
}
