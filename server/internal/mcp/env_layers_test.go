package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// layeredEnv answers the system environment and one selected saved environment.
type layeredEnv struct {
	system map[string]string
	saved  map[string]string
}

func (e layeredEnv) Load(context.Context, uuid.UUID, toolconfig.SlugOrID) (map[string]string, error) {
	return e.saved, nil
}

func (e layeredEnv) LoadSystemEnv(context.Context, uuid.UUID, uuid.UUID, string, string) (*toolconfig.CaseInsensitiveEnv, error) {
	return toolconfig.CIEnvFrom(e.system), nil
}

// countingServer answers every request with body and counts them.
func countingServer(t *testing.T, body string) (string, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL, &hits
}

// System environment values go to the server URL system environment names, whichever saved environment the request selects.
func TestSystemEnvServerURLHoldsAcrossSelectedEnvironment(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// systemURL says whether system environment sets the server URL as well as the key.
		systemURL bool
	}{
		{name: "system key and server URL", systemURL: true},
		{name: "system key only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			auth := requireProjectAuthContext(t, ctx)
			ts := createPublicMCPToolset(t, ctx, ti, auth, "env-layers-"+uuid.NewString()[:8])
			ti.addToolWithSecurity(ctx, t, ts.ID, *auth.ProjectID, auth.ActiveOrganizationID)

			systemURL, systemHits := countingServer(t, `{"ok":true}`)
			otherURL, otherHits := countingServer(t, `{"ok":true}`)
			system := map[string]string{"TEST_API_KEY": "system-key"}
			if tc.systemURL {
				system["TEST_SERVER_URL"] = systemURL
			}
			ti.service.SetEnvironmentLoader(layeredEnv{system: system, saved: map[string]string{"TEST_SERVER_URL": otherURL}})

			_, err := ti.service.HandleToolsCall(ctx, &mcp.McpInputs{
				ProjectID:       *auth.ProjectID,
				Toolset:         ts.Slug,
				Environment:     "selected",
				Authenticated:   true,
				McpEnvVariables: map[string]string{"test_server_url": otherURL},
				Mode:            mcp.ToolModeStatic,
			}, "test_tool", json.RawMessage(`{}`))

			require.Zero(t, otherHits.Load())
			if tc.systemURL {
				require.NoError(t, err)
				require.Equal(t, int64(1), systemHits.Load())
				return
			}
			// The tool has no server URL of its own, so nothing is called.
			require.Error(t, err)
			require.Zero(t, systemHits.Load())
		})
	}
}

// A system client asks for its token at the token URL system environment or the tool names, whichever saved environment the request selects.
func TestSystemClientTokenURLHoldsAcrossSelectedEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	auth := requireProjectAuthContext(t, ctx)
	ts := createPublicMCPToolset(t, ctx, ti, auth, "env-layers-"+uuid.NewString()[:8])

	tokenURL, tokenHits := countingServer(t, `{"access_token":"machine-token","expires_in":60}`)
	otherURL, otherHits := countingServer(t, `{"access_token":"other-token","expires_in":60}`)
	ti.addClientCredentialsTool(ctx, t, ts.ID, *auth.ProjectID, auth.ActiveOrganizationID, tokenURL+"/oauth/token")
	ti.service.SetEnvironmentLoader(layeredEnv{
		system: map[string]string{"TEST_SERVER_URL": toolServer(t).URL, "MACHINE_CLIENT_ID": "machine-client", "MACHINE_CLIENT_SECRET": "machine-secret"},
		saved:  map[string]string{"MACHINE_TOKEN_URL": otherURL + "/oauth/token"},
	})

	_, err := ti.service.HandleToolsCall(ctx, &mcp.McpInputs{
		ProjectID:     *auth.ProjectID,
		Toolset:       ts.Slug,
		Environment:   "selected",
		Authenticated: true,
		Mode:          mcp.ToolModeStatic,
	}, "machine_tool", json.RawMessage(`{}`))
	require.NoError(t, err)

	require.Zero(t, otherHits.Load())
	require.Equal(t, int64(1), tokenHits.Load())
}

// A variable set to system-provided is not taken from a request header, on a server with or without a default environment.
func TestSystemProvidedVariableNotTakenFromHeader(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		providedBy string
		want       string
	}{
		{name: "no setting", providedBy: "", want: "header-key"},
		{name: "user provided", providedBy: "user", want: "header-key"},
		{name: "system provided", providedBy: "system", want: "system-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			auth := requireProjectAuthContext(t, ctx)
			ts := createPublicMCPToolset(t, ctx, ti, auth, "env-layers-"+uuid.NewString()[:8])
			ti.addToolWithSecurity(ctx, t, ts.ID, *auth.ProjectID, auth.ActiveOrganizationID)
			if tc.providedBy != "" {
				setSecurityVariableProvidedBy(t, ctx, ti, ts.ID, *auth.ProjectID, tc.providedBy)
			}

			var sentKey atomic.Value
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sentKey.Store(r.Header.Get("X-Api-Key"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			t.Cleanup(upstream.Close)
			ti.service.SetEnvironmentLoader(layeredEnv{system: map[string]string{"TEST_SERVER_URL": upstream.URL, "TEST_API_KEY": "system-key"}, saved: nil})

			resp, err := servePublicHTTP(t, t.Context(), ti, ts.McpSlug.String, makeToolsCallBody("test_tool"), "", map[string]string{"Mcp-Test-Api-Key": "header-key"})
			require.NoError(t, err)
			_, isError := metaToolResultText(t, decodeRPCResponse(t, resp))
			require.False(t, isError)
			require.Equal(t, tc.want, sentKey.Load())
		})
	}
}
