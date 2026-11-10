package remotemcp_test

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func debugSteps(report *remotemcp.DebugReport) map[string]*remotemcp.DebugStep {
	steps := map[string]*remotemcp.DebugStep{}
	for _, step := range report.Steps {
		steps[step.ID] = step
	}
	return steps
}

func TestDebugRemoteMcpURL_ListsToolsAndPromptsWithoutAuth(t *testing.T) {
	t.Parallel()
	server := mcp.NewServer(&mcp.Implementation{Name: "upstream", Version: "1.2.3"}, &mcp.ServerOptions{Instructions: "Use me wisely."})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_things",
		Description: "Lists things.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{}, nil, nil
	})
	server.AddPrompt(&mcp.Prompt{Name: "greet", Description: "Says hello."}, func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{}, nil
	})
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(upstream.Close)

	report := remotemcp.DebugRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL, remotemcp.DebugOptions{ClientMetadataOrigin: nil})

	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, report.Outcome)
	require.Equal(t, remotemcp.DebugAuthMethodNone, report.AuthMethod)
	require.Equal(t, "upstream", requireValue(t, report.ServerName))
	require.Equal(t, "1.2.3", requireValue(t, report.ServerVersion))
	require.Equal(t, "Use me wisely.", requireValue(t, report.Instructions))
	require.NotEmpty(t, requireValue(t, report.ProtocolVersion))
	require.Len(t, report.Tools, 1)
	require.Equal(t, "list_things", report.Tools[0].Name)
	require.NotNil(t, report.Tools[0].Annotations)
	require.Len(t, report.Prompts, 1)
	require.Equal(t, "greet", report.Prompts[0].Name)

	steps := debugSteps(report)
	require.Equal(t, remotemcp.DebugStatusPassed, steps[remotemcp.DebugStepReachServer].Status)
	require.Equal(t, remotemcp.DebugStatusSkipped, steps[remotemcp.DebugStepParseAuthChallenge].Status)
	require.Equal(t, remotemcp.DebugStatusPassed, steps[remotemcp.DebugStepInitialize].Status)
	require.Equal(t, remotemcp.DebugStatusPassed, steps[remotemcp.DebugStepListCapabilities].Status)
}

func TestDebugRemoteMcpURL_TracesOAuthDiscovery(t *testing.T) {
	t.Parallel()
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp":
			w.Header().Set("Content-Type", "application/json")
			// An http issuer fails RFC 8414 discovery, which requires HTTPS.
			_, _ = fmt.Fprintf(w, `{"resource":%q,"authorization_servers":[%q]}`, upstream.URL+"/mcp", upstream.URL)
		default:
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp", scope="read"`, upstream.URL))
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(upstream.Close)

	report := remotemcp.DebugRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL+"/mcp", remotemcp.DebugOptions{ClientMetadataOrigin: nil})

	require.Equal(t, remotemcp.ProbeOutcomeAuthenticationRequired, report.Outcome)
	require.Equal(t, remotemcp.DebugAuthMethodUnknown, report.AuthMethod)
	steps := debugSteps(report)
	require.Equal(t, remotemcp.DebugStatusPassed, steps[remotemcp.DebugStepReachServer].Status)
	require.Equal(t, http.StatusUnauthorized, requireValue(t, steps[remotemcp.DebugStepReachServer].HTTPStatus))
	require.Equal(t, remotemcp.DebugStatusPassed, steps[remotemcp.DebugStepParseAuthChallenge].Status)
	require.Equal(t, []string{"read"}, steps[remotemcp.DebugStepParseAuthChallenge].Details["scopes"])
	require.Equal(t, remotemcp.DebugStatusPassed, steps[remotemcp.DebugStepFetchProtectedRes].Status)
	require.Equal(t, remotemcp.DebugStatusFailed, steps[remotemcp.DebugStepFetchAuthServer].Status)
	require.Equal(t, remotemcp.DebugStatusFailed, steps[remotemcp.DebugStepDetectAuthMethod].Status)
	require.Equal(t, remotemcp.DebugStatusSkipped, steps[remotemcp.DebugStepInitialize].Status)
	require.Equal(t, remotemcp.DebugStatusSkipped, steps[remotemcp.DebugStepListCapabilities].Status)
	require.Empty(t, report.Tools)
}

func TestDebugRemoteMcpURL_DetectsNonOAuthChallenge(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="upstream"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	report := remotemcp.DebugRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL, remotemcp.DebugOptions{ClientMetadataOrigin: nil})

	require.Equal(t, remotemcp.ProbeOutcomeAuthenticationRequired, report.Outcome)
	require.Equal(t, remotemcp.DebugAuthMethodNonOAuth, report.AuthMethod)
	steps := debugSteps(report)
	require.Equal(t, remotemcp.DebugStatusWarning, steps[remotemcp.DebugStepParseAuthChallenge].Status)
	require.Equal(t, remotemcp.DebugStatusSkipped, steps[remotemcp.DebugStepFetchProtectedRes].Status)
	require.Equal(t, remotemcp.DebugStatusPassed, steps[remotemcp.DebugStepDetectAuthMethod].Status)
}

func TestDebugRemoteMcpURL_InvalidMCPResponse(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not mcp</html>"))
	}))
	t.Cleanup(upstream.Close)

	report := remotemcp.DebugRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL, remotemcp.DebugOptions{ClientMetadataOrigin: nil})

	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, report.Outcome)
	steps := debugSteps(report)
	require.Equal(t, remotemcp.DebugStatusPassed, steps[remotemcp.DebugStepReachServer].Status)
	require.Equal(t, remotemcp.DebugStatusFailed, steps[remotemcp.DebugStepInitialize].Status)
	require.Equal(t, remotemcp.DebugStatusSkipped, steps[remotemcp.DebugStepListCapabilities].Status)
}

// newCIMDUpstream serves an MCP endpoint behind OAuth whose authorization
// server advertises both Client ID Metadata Documents and DCR, over TLS so
// RFC 8414 discovery accepts the issuer.
func newCIMDUpstream(t *testing.T) (*httptest.Server, *guardian.Policy) {
	t.Helper()
	var upstream *httptest.Server
	upstream = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp":
			_, _ = fmt.Fprintf(w, `{"resource":%q,"authorization_servers":[%q]}`, upstream.URL+"/mcp", upstream.URL)
		case "/.well-known/oauth-authorization-server":
			_, _ = fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"registration_endpoint":%q,"client_id_metadata_document_supported":true,"code_challenge_methods_supported":["S256"]}`,
				upstream.URL, upstream.URL+"/authorize", upstream.URL+"/token", upstream.URL+"/register")
		default:
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, upstream.URL))
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(upstream.Close)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)
	return upstream, policy
}

func TestDebugRemoteMcpURL_WarnsWhenProviderCannotFetchClientMetadata(t *testing.T) {
	t.Parallel()
	upstream, policy := newCIMDUpstream(t)
	origin, err := url.Parse("https://localhost:27699")
	require.NoError(t, err)

	report := remotemcp.DebugRemoteMcpURL(t.Context(), policy, upstream.URL+"/mcp", remotemcp.DebugOptions{ClientMetadataOrigin: origin})

	require.Equal(t, remotemcp.DebugAuthMethodCIMD, report.AuthMethod)
	detect := debugSteps(report)[remotemcp.DebugStepDetectAuthMethod]
	require.Equal(t, remotemcp.DebugStatusWarning, detect.Status)
	require.Contains(t, detect.Summary, "localhost:27699")
	require.Contains(t, detect.Summary, "Choose DCR")
}

func TestDebugRemoteMcpURL_PublicOriginPassesClientMetadataCheck(t *testing.T) {
	t.Parallel()
	upstream, policy := newCIMDUpstream(t)
	origin, err := url.Parse("https://app.example.com")
	require.NoError(t, err)

	report := remotemcp.DebugRemoteMcpURL(t.Context(), policy, upstream.URL+"/mcp", remotemcp.DebugOptions{ClientMetadataOrigin: origin})

	require.Equal(t, remotemcp.DebugAuthMethodCIMD, report.AuthMethod)
	require.Equal(t, remotemcp.DebugStatusPassed, debugSteps(report)[remotemcp.DebugStepDetectAuthMethod].Status)
}

func TestDebugRemoteMcpURL_BlockedHostIsUnreachableTrace(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))

	report := remotemcp.DebugRemoteMcpURL(t.Context(), policy, "https://127.0.0.1/mcp", remotemcp.DebugOptions{ClientMetadataOrigin: nil})

	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, report.Outcome)
	require.Len(t, report.Steps, 7)
	steps := debugSteps(report)
	require.Equal(t, remotemcp.DebugStatusFailed, steps[remotemcp.DebugStepReachServer].Status)
	require.Equal(t, remotemcp.DebugStatusSkipped, steps[remotemcp.DebugStepInitialize].Status)
}
