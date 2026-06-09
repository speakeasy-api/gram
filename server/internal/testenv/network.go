package testenv

import (
	"context"
	"fmt"
	"net"

	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
)

// candidateAddrs returns host-reachable addresses for a container port, most
// preferred first. The container IP bypasses Docker's port-publishing path
// (port allocation, NAT rules, userland proxy), which under heavy parallel
// container churn in CI can route a published-port dial to an unrelated
// process on the host. Container IPs are only routable when tests and Docker
// share a network stack (Linux), so the published port is kept as a fallback
// for environments like Docker Desktop.
func candidateAddrs(ctx context.Context, container testcontainers.Container, port nat.Port) ([]string, error) {
	addrs := make([]string, 0, 2)

	if ip, err := container.ContainerIP(ctx); err == nil && ip != "" {
		addrs = append(addrs, net.JoinHostPort(ip, port.Port()))
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("get container host: %w", err)
	}

	mapped, err := container.MappedPort(ctx, string(port))
	if err != nil {
		return nil, fmt.Errorf("get mapped port for %s: %w", port, err)
	}

	// Avoid a DNS lookup for localhost inside synctest bubbles without
	// changing arbitrary Docker/Testcontainers endpoints. Re-resolving the
	// host here can pick an address that is not the actual published
	// endpoint (for example ::1 instead of Docker's IPv4 localhost binding).
	if host == "localhost" {
		host = "127.0.0.1"
	}

	addrs = append(addrs, net.JoinHostPort(host, mapped.Port()))

	return addrs, nil
}
