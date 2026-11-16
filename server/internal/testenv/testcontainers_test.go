package testenv

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestRetryDockerInfoRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		err := retryDockerInfo(t.Context(), func(context.Context) error {
			attempts++
			if attempts < 3 {
				return errors.New("docker unavailable")
			}
			return nil
		})

		require.NoError(t, err)
		require.Equal(t, 3, attempts)
	})
}

func TestRetryDockerInfoStopsAtDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		err := retryDockerInfo(ctx, func(context.Context) error {
			return errors.New("docker unavailable")
		})

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorContains(t, err, "docker info after retries")
	})
}

func TestTestContainersBindPublishedPortsToLoopback(t *testing.T) {
	t.Parallel()
	// A remote daemon keeps Docker's default binding, since tests dial its host.
	wantLoopback := localDockerHost()

	container, _, err := NewTestRedis(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })

	info, err := container.Inspect(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, info.HostConfig.PortBindings, "testcontainers publishes the container's exposed ports")
	for port, bindings := range info.HostConfig.PortBindings {
		for _, binding := range bindings {
			// Docker binds an unset host IP on every interface.
			hostIP := "all interfaces"
			if binding.HostIP.IsValid() {
				hostIP = binding.HostIP.String()
			}
			require.Equal(t, wantLoopback, binding.HostIP.IsLoopback(), "%s is published on %s", port, hostIP)
		}
	}
}

func TestWithLoopbackPortsKeepsEarlierBindingsOnLoopback(t *testing.T) {
	t.Parallel()
	// A remote daemon keeps Docker's default binding, since tests dial its host.
	wantLoopback := localDockerHost()

	pinned := network.MustParsePort("6379/tcp")
	req := testcontainers.GenericContainerRequest{}
	req.ExposedPorts = []string{"6379/tcp", "5432/tcp"}
	req.HostConfigModifier = func(hostConfig *container.HostConfig) {
		hostConfig.PortBindings = network.PortMap{pinned: {{HostPort: "16379"}}}
	}
	require.NoError(t, WithLoopbackPorts()(&req))

	hostConfig := &container.HostConfig{}
	req.HostConfigModifier(hostConfig)

	require.Equal(t, "16379", hostConfig.PortBindings[pinned][0].HostPort, "an earlier modifier's host port is kept")
	require.Equal(t, wantLoopback, hostConfig.PortBindings[pinned][0].HostIP.IsLoopback())
	if wantLoopback {
		added := hostConfig.PortBindings[network.MustParsePort("5432/tcp")]
		require.Len(t, added, 1)
		require.True(t, added[0].HostIP.IsLoopback())
	}
}
