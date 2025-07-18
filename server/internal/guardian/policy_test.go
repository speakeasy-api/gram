package guardian_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/stretchr/testify/require"
)

func TestNewUnsafePolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		cidrBlocks  []string
		expectError bool
	}{
		{
			name:        "valid CIDR blocks",
			cidrBlocks:  []string{"10.0.0.0/8", "192.168.0.0/16"},
			expectError: false,
		},
		{
			name:        "empty CIDR blocks",
			cidrBlocks:  []string{},
			expectError: false,
		},
		{
			name:        "invalid CIDR block",
			cidrBlocks:  []string{"invalid-cidr"},
			expectError: true,
		},
		{
			name:        "mixed valid and invalid CIDR blocks",
			cidrBlocks:  []string{"10.0.0.0/8", "invalid-cidr"},
			expectError: true,
		},
		{
			name:        "IPv6 CIDR blocks",
			cidrBlocks:  []string{"2001:db8::/32", "::1/128"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy, err := guardian.NewUnsafePolicy(tt.cidrBlocks)
			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, policy)
			} else {
				require.NoError(t, err)
				require.NotNil(t, policy)
			}
		})
	}
}

func TestPolicy_Dialer(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy()
	dialer := policy.Dialer()

	require.NotNil(t, dialer)
	require.Equal(t, 30*time.Second, dialer.Timeout)
	require.Equal(t, 30*time.Second, dialer.KeepAlive)
	require.NotNil(t, dialer.ControlContext)
}

func TestPolicy_DialerControlContext(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy()
	dialer := policy.Dialer()

	ctx := t.Context()

	tests := []struct {
		name          string
		address       string
		expectedError error
	}{
		{
			name:          "blocked private IP",
			address:       "192.168.1.1:80",
			expectedError: guardian.ErrBlockedIP,
		},
		{
			name:          "blocked loopback IP",
			address:       "127.0.0.1:80",
			expectedError: guardian.ErrBlockedIP,
		},
		{
			name:          "invalid address format",
			address:       "invalid-address",
			expectedError: guardian.ErrBadHost,
		},
		{
			name:          "non-IP host",
			address:       "example.com:80",
			expectedError: guardian.ErrBadHost,
		},
		{
			name:          "allowed public IP",
			address:       "8.8.8.8:80",
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := dialer.ControlContext(ctx, "tcp", tt.address, nil)
			if tt.expectedError != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPolicy_DialerControlContext_CustomPolicy(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy([]string{"8.8.8.0/24"})
	require.NoError(t, err)

	dialer := policy.Dialer()
	ctx := t.Context()

	err = dialer.ControlContext(ctx, "tcp", "8.8.8.8:80", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, guardian.ErrBlockedIP)

	err = dialer.ControlContext(ctx, "tcp", "1.1.1.1:80", nil)
	require.NoError(t, err)
}

func TestPolicy_Client(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy()
	client := policy.Client()

	require.NotNil(t, client)
	require.NotNil(t, client.Transport)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.DialContext)
}

func TestPolicy_PooledClient(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy()
	client := policy.PooledClient()

	require.NotNil(t, client)
	require.NotNil(t, client.Transport)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.DialContext)
}

func TestDefaultPolicyBlocksPrivateIPs(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy()
	dialer := policy.Dialer()
	ctx := t.Context()

	privateIPv4Blocks := []string{
		"10.0.0.1:80",
		"172.16.0.1:80",
		"192.168.1.1:80",
	}

	for _, address := range privateIPv4Blocks {
		t.Run("blocks "+address, func(t *testing.T) {
			t.Parallel()
			err := dialer.ControlContext(ctx, "tcp", address, nil)
			require.Error(t, err)
			require.ErrorIs(t, err, guardian.ErrBlockedIP)
		})
	}
}

func TestPolicy_DialerIPBlocking(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy()
	dialer := policy.Dialer()
	ctx := t.Context()

	blockedIPs := []string{
		"10.0.0.1:80",
		"172.16.0.1:80",
		"192.168.1.1:80",
		"127.0.0.1:80",
		"[::1]:80",
	}

	for _, ip := range blockedIPs {
		t.Run("blocks "+ip, func(t *testing.T) {
			t.Parallel()
			err := dialer.ControlContext(ctx, "tcp", ip, nil)
			require.Error(t, err)
			require.ErrorIs(t, err, guardian.ErrBlockedIP)
		})
	}
}

func TestPolicy_DialerEdgeCases(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy()
	dialer := policy.Dialer()
	ctx := t.Context()

	tests := []struct {
		name        string
		address     string
		expectError bool
		errorType   error
	}{
		{
			name:        "missing port",
			address:     "192.168.1.1",
			expectError: true,
			errorType:   guardian.ErrBadHost,
		},
		{
			name:        "IPv6 with brackets",
			address:     "[::1]:80",
			expectError: true,
			errorType:   guardian.ErrBlockedIP,
		},
		{
			name:        "hostname instead of IP",
			address:     "localhost:80",
			expectError: true,
			errorType:   guardian.ErrBadHost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := dialer.ControlContext(ctx, "tcp", tt.address, nil)
			if tt.expectError {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.errorType)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPolicy_DialerContext(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy()
	dialer := policy.Dialer()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Millisecond)
	defer cancel()

	time.Sleep(2 * time.Millisecond)

	err := dialer.ControlContext(ctx, "tcp", "8.8.8.8:80", nil)
	require.NoError(t, err)
}

func TestPolicy_HTTPClientWithCustomPolicy(t *testing.T) {
	t.Parallel()

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "test response")
	}))
	defer server.Close()

	// Extract IP from server URL
	serverURL := server.URL
	host, _, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)

	// Create a custom policy that blocks the test server's IP
	customPolicy, err := guardian.NewUnsafePolicy([]string{host + "/32"})
	require.NoError(t, err)

	// Test that the custom policy blocks the server
	client := customPolicy.Client()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, serverURL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.Error(t, err)
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Nil(t, resp)
}

func TestPolicy_PooledHTTPClientWithFakeNetwork(t *testing.T) {
	t.Parallel()

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "pooled client response")
	}))
	defer server.Close()

	// Extract the server's IP address and create a custom policy that allows it
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)

	// Create a custom policy that allows the test server but blocks private IPs
	policy, err := guardian.NewUnsafePolicy([]string{
		"192.168.0.0/16", // Block private IPs
		"10.0.0.0/8",     // Block private IPs
		"172.16.0.0/12",  // Block private IPs
		// Note: We don't block 127.0.0.1 to allow the test server
	})
	require.NoError(t, err)

	client := policy.PooledClient()

	// Test successful request to the test server
	serverURL := fmt.Sprintf("http://%s:%s", host, port)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, serverURL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	// Test blocked request to private IP
	blockedReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://192.168.1.1:8080", nil)
	require.NoError(t, err)

	blockedResp, err := client.Do(blockedReq)
	require.Error(t, err)
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
	if blockedResp != nil {
		require.NoError(t, blockedResp.Body.Close())
	}
	require.Nil(t, blockedResp)
}

func TestPolicy_IPv4MappedIPv6Addresses(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy()
	dialer := policy.Dialer()
	ctx := t.Context()

	// Test various IPv4-mapped IPv6 addresses that should be blocked
	ipv4MappedAddresses := []struct {
		name    string
		address string
		desc    string
	}{
		{
			name:    "IPv4-mapped loopback",
			address: "[::ffff:127.0.0.1]:80",
			desc:    "IPv4-mapped representation of 127.0.0.1",
		},
		{
			name:    "IPv4-mapped private 10.x",
			address: "[::ffff:10.0.0.1]:80",
			desc:    "IPv4-mapped representation of 10.0.0.1",
		},
		{
			name:    "IPv4-mapped private 192.168.x",
			address: "[::ffff:192.168.1.1]:80",
			desc:    "IPv4-mapped representation of 192.168.1.1",
		},
		{
			name:    "IPv4-mapped private 172.16.x",
			address: "[::ffff:172.16.0.1]:80",
			desc:    "IPv4-mapped representation of 172.16.0.1",
		},
		{
			name:    "IPv4-mapped link-local",
			address: "[::ffff:169.254.1.1]:80",
			desc:    "IPv4-mapped representation of 169.254.1.1",
		},
		{
			name:    "IPv4-mapped broadcast",
			address: "[::ffff:255.255.255.255]:80",
			desc:    "IPv4-mapped representation of broadcast address",
		},
		{
			name:    "IPv4-mapped zero address",
			address: "[::ffff:0.0.0.0]:80",
			desc:    "IPv4-mapped representation of 0.0.0.0",
		},
	}

	for _, tt := range ipv4MappedAddresses {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := dialer.ControlContext(ctx, "tcp", tt.address, nil)
			require.Error(t, err, "Expected error for %s (%s)", tt.address, tt.desc)
			require.ErrorIs(t, err, guardian.ErrBlockedIP, "Expected ErrBlockedIP for %s", tt.address)
		})
	}

	// Test that IPv4-mapped public addresses are allowed
	publicIPv4Mapped := []struct {
		name    string
		address string
	}{
		{
			name:    "IPv4-mapped Google DNS",
			address: "[::ffff:8.8.8.8]:80",
		},
		{
			name:    "IPv4-mapped Cloudflare DNS",
			address: "[::ffff:1.1.1.1]:80",
		},
	}

	for _, tt := range publicIPv4Mapped {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := dialer.ControlContext(ctx, "tcp", tt.address, nil)
			require.NoError(t, err, "Expected no error for public IPv4-mapped address %s", tt.address)
		})
	}
}

func TestPolicy_IPv6VariationsBlocking(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy()
	dialer := policy.Dialer()
	ctx := t.Context()

	// Test various IPv6 address representations that should be blocked
	blockedIPv6Variations := []struct {
		name    string
		address string
		desc    string
	}{
		{
			name:    "IPv6 loopback short form",
			address: "[::1]:80",
			desc:    "IPv6 loopback address",
		},
		{
			name:    "IPv6 loopback full form",
			address: "[0000:0000:0000:0000:0000:0000:0000:0001]:80",
			desc:    "IPv6 loopback address in full form",
		},
		{
			name:    "IPv6 link-local",
			address: "[fe80::1]:80",
			desc:    "IPv6 link-local address",
		},
		{
			name:    "IPv6 unique local",
			address: "[fc00::1]:80",
			desc:    "IPv6 unique local address",
		},
		{
			name:    "IPv6 multicast",
			address: "[ff02::1]:80",
			desc:    "IPv6 multicast address",
		},
		{
			name:    "IPv6 unspecified",
			address: "[::]:80",
			desc:    "IPv6 unspecified address",
		},
	}

	for _, tt := range blockedIPv6Variations {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := dialer.ControlContext(ctx, "tcp", tt.address, nil)
			require.Error(t, err, "Expected error for %s (%s)", tt.address, tt.desc)
			require.ErrorIs(t, err, guardian.ErrBlockedIP, "Expected ErrBlockedIP for %s", tt.address)
		})
	}
}
