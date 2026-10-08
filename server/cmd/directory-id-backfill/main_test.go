package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestWorkOSPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		endpoint    string
		environment string
		invalid     bool
		local       bool
	}{
		{name: "default", endpoint: "", environment: "production"},
		{name: "external https", endpoint: "https://api.workos.com", environment: "production"},
		{name: "external https in local mode", endpoint: "https://api.workos.com", environment: "local"},
		{name: "external http", endpoint: "http://api.workos.com", environment: "production", invalid: true},
		{name: "external http in local mode", endpoint: "http://api.workos.com", environment: "local", invalid: true},
		{name: "local stub", endpoint: "http://127.0.0.1:5556", environment: "local", local: true},
		{name: "localhost stub", endpoint: "http://localhost:5556", environment: "local", local: true},
		{name: "ipv6 stub", endpoint: "http://[::1]:5556", environment: "local", local: true},
		{name: "local https", endpoint: "https://localhost:5556", environment: "local", local: true},
		{name: "loopback http in production", endpoint: "http://127.0.0.1:5556", environment: "production", invalid: true},
		{name: "loopback http without environment", endpoint: "http://localhost:5556", invalid: true},
		{name: "loopback https in production stays blocked", endpoint: "https://localhost:5556", environment: "production"},
		{name: "private http in local mode", endpoint: "http://10.0.0.1", environment: "local", invalid: true},
		{name: "relative URL", endpoint: "/workos", environment: "local", invalid: true},
		{name: "credentials", endpoint: "https://user:password@api.workos.com", invalid: true},
		{name: "query", endpoint: "https://api.workos.com?key=value", invalid: true},
		{name: "unsupported scheme", endpoint: "file://localhost/workos", environment: "local", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			policy, err := workOSPolicy(tc.endpoint, tc.environment)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			err = policy.Dialer().ControlContext(t.Context(), "tcp", "127.0.0.1:5556", nil)
			if tc.local {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, guardian.ErrBlockedIP)
			}
		})
	}
}

func TestWorkOSPolicyAllowsLocalInventory(t *testing.T) {
	t.Parallel()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/directories", r.URL.Path)
		assert.Equal(t, "Bearer local-stub-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprint(w, `{"data":[],"list_metadata":{"before":"","after":""}}`)
		assert.NoError(t, err)
	}))
	defer stub.Close()
	policy, err := workOSPolicy(stub.URL, "local")
	require.NoError(t, err)
	client := workos.NewClient(policy, "local-stub-key", workos.ClientOpts{Endpoint: stub.URL, ClientID: ""})
	directories, err := client.ListDirectories(t.Context(), "org_local_stub")
	require.NoError(t, err)
	require.Empty(t, directories)
}
