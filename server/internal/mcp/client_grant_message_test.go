package mcp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	tools_repo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// tokenHost is a token URL host the mock resolver answers for.
const tokenHost = "token.example.invalid"

var networkDetail = regexp.MustCompile(`https?://|\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}|:\d{2,5}\b|` + regexp.QuoteMeta(tokenHost))

// addClientCredentialsTool adds an HTTP tool whose oauth2 client credentials flow requests tokens from tokenURL.
func (ti *testInstance) addClientCredentialsTool(ctx context.Context, t *testing.T, toolsetID, projectID uuid.UUID, orgID, tokenURL string) {
	t.Helper()

	deployments := deployments_repo.New(ti.conn)
	deploymentID, err := deployments.InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{ProjectID: projectID, OrganizationID: orgID, UserID: "test-user", IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	require.NoError(t, deployments.CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{DeploymentID: deploymentID, Status: "completed"}))

	toolURN := urn.NewTool(urn.ToolKindHTTP, "machine-api", uuid.NewString()[:8])
	require.NoError(t, tools_repo.New(ti.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
		ProjectID:       projectID,
		DeploymentID:    deploymentID,
		ToolUrn:         toolURN,
		Name:            "machine_tool",
		UntruncatedName: pgtype.Text{},
		Summary:         "Machine tool",
		Description:     "A tool that signs in with client credentials",
		Tags:            []string{},
		HttpMethod:      "GET",
		Path:            "/test",
		SchemaVersion:   "3.0.0",
		Schema:          []byte(`{}`),
		ServerEnvVar:    "TEST_SERVER_URL",
		Security:        []byte(`[{"machine": []}]`),
		HeaderSettings:  []byte(`{}`),
		QuerySettings:   []byte(`{}`),
		PathSettings:    []byte(`{}`),
		ReadOnlyHint:    pgtype.Bool{},
		DestructiveHint: pgtype.Bool{},
		IdempotentHint:  pgtype.Bool{},
		OpenWorldHint:   pgtype.Bool{},
	}))

	flows, err := json.Marshal(map[string]any{"clientCredentials": map[string]any{"tokenUrl": tokenURL}})
	require.NoError(t, err)
	_, err = deployments.CreateHTTPSecurity(ctx, deployments_repo.CreateHTTPSecurityParams{
		Key:                 "machine",
		DeploymentID:        deploymentID,
		ProjectID:           uuid.NullUUID{UUID: projectID, Valid: true},
		Openapiv3DocumentID: uuid.NullUUID{},
		Type:                pgtype.Text{String: "oauth2", Valid: true},
		Name:                pgtype.Text{},
		InPlacement:         pgtype.Text{},
		Scheme:              pgtype.Text{},
		BearerFormat:        pgtype.Text{},
		EnvVariables:        []string{"MACHINE_CLIENT_ID", "MACHINE_CLIENT_SECRET", "MACHINE_TOKEN_URL"},
		OauthTypes:          []string{"client_credentials"},
		OauthFlows:          flows,
	})
	require.NoError(t, err)

	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolsetID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)
}

type clientGrantCall struct {
	text    string
	isError bool
	stored  string
	logs    string
	rec     *mcpconnectionattemptstest.Recording
}

// callClientCredentialsTool calls a tool whose token URL is tokenURL, as an admin set it up, with headers the caller sent, and returns what each surface kept.
func callClientCredentialsTool(t *testing.T, tokenURL, serverURL string, headers map[string]string, guardianOpts ...func(*guardian.Policy)) clientGrantCall {
	t.Helper()

	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{AddSource: false, Level: slog.LevelDebug, ReplaceAttr: nil}))
	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, logger, testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, nil, guardianOpts...)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	ti.service.SetTelemetryLogger(telemetry.NewLogger(ctx, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), chConn, enabled, enabled, nil, telemetry.NewNoopLogPublisher(testenv.NewLogger(t))))

	auth := requireProjectAuthContext(t, ctx)
	ts := createPublicMCPToolset(t, ctx, ti, auth, "client-grant-"+uuid.NewString()[:8])
	ti.addClientCredentialsTool(ctx, t, ts.ID, *auth.ProjectID, auth.ActiveOrganizationID, tokenURL)
	ti.service.SetEnvironmentLoader(savedKeyEnv{vars: map[string]string{"TEST_SERVER_URL": serverURL, "MACHINE_CLIENT_ID": "machine-client", "MACHINE_CLIENT_SECRET": "machine-secret"}})
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	resp, err := servePublicHTTP(t, t.Context(), ti, ts.McpSlug.String, makeToolsCallBody("machine_tool"), "", headers)
	require.NoError(t, err)
	text, isError := metaToolResultText(t, decodeRPCResponse(t, resp))

	var stored string
	require.Eventually(t, func() bool {
		return chConn.QueryRow(t.Context(), `SELECT concat(toJSONString(attributes), ' ', body) FROM telemetry_logs
			WHERE gram_project_id = ? AND event_source = 'tool_call' AND tool_name = 'machine_tool' LIMIT 1`,
			auth.ProjectID.String()).Scan(&stored) == nil
	}, 5*time.Second, 50*time.Millisecond, "tool call log row did not appear")

	return clientGrantCall{text: text, isError: isError, stored: stored, logs: logs.String(), rec: rec}
}

func toolErrorMessage(t *testing.T, text string) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &body), text)
	return body.Error
}

func toolServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authorization":"` + r.Header.Get("Authorization") + `"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// A failed token request tells the caller what to check, and keeps the request's details in the server log only.
func TestClientGrantFailureMessage(t *testing.T) {
	t.Parallel()

	answering := func(t *testing.T, status int, body string) string {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(server.Close)
		return server.URL + "/oauth/token"
	}
	resolving := func(err error) func(*guardian.Policy) {
		return guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(ctx context.Context, network, host string) ([]net.IP, error) {
			if host == tokenHost {
				return nil, err
			}
			return net.DefaultResolver.LookupIP(ctx, network, host)
		}}))
	}

	const (
		prefix    = "The tool could not get its access token: "
		checkURL  = " Check the tool's token URL. If you did not set this tool up, ask your administrator."
		unreached = prefix + "its token service could not be reached or answered with an error. If trying again does not help, check the tool's token URL. If you did not set this tool up, ask your administrator."
		refused   = prefix + "its token service refused the request. Check the tool's client ID, client secret, scopes and token URL. If you did not set this tool up, ask your administrator."
		unusable  = prefix + "its token service gave an answer Speakeasy could not use." + checkURL
	)
	blocked := prefix + "its token URL " + guardian.AddressNotAllowed.Problem() + "."
	missing := prefix + "its token URL " + guardian.AddressNotFound.Problem() + "."
	callerClient := map[string]string{"Mcp-Machine-Client-Id": "caller-client", "Mcp-Machine-Client-Secret": "caller-secret"}

	for _, tc := range []struct {
		name     string
		tokenURL func(t *testing.T) string
		headers  map[string]string
		options  []func(*guardian.Policy)
		secret   []string
		logged   string
		want     string
		code     mcpconnectionattempts.Code
	}{
		{
			name:     "refused address",
			tokenURL: func(*testing.T) string { return "https://192.0.2.10:8443/oauth/token" },
			options:  []func(*guardian.Policy){guardian.WithBlockedCIDRBlocks("192.0.2.0/24")},
			secret:   []string{"192.0.2.10", "8443"},
			want:     blocked + checkURL,
			code:     mcpconnectionattempts.CodeAddressNotAllowed,
		},
		{
			name:     "refused address the caller set",
			tokenURL: func(*testing.T) string { return "https://" + tokenHost + "/unused" },
			headers:  map[string]string{"Mcp-Machine-Token-Url": "https://192.0.2.10:8443/oauth/token"},
			options:  []func(*guardian.Policy){guardian.WithBlockedCIDRBlocks("192.0.2.0/24")},
			secret:   []string{"192.0.2.10", "8443"},
			logged:   "192.0.2.10",
			want:     blocked + checkURL,
			code:     mcpconnectionattempts.CodeAddressNotAllowed,
		},
		{
			name:     "missing host",
			tokenURL: func(*testing.T) string { return "https://" + tokenHost + ":8443/oauth/token" },
			options:  []func(*guardian.Policy){resolving(&net.DNSError{Err: "no such host", Name: tokenHost, Server: "198.51.100.53:53", IsNotFound: true})},
			secret:   []string{tokenHost, "198.51.100.53", "8443"},
			want:     missing + checkURL,
			code:     mcpconnectionattempts.CodeAddressNotFound,
		},
		{
			name:     "temporary lookup failure",
			tokenURL: func(*testing.T) string { return "https://" + tokenHost + ":8443/oauth/token" },
			options:  []func(*guardian.Policy){resolving(&net.DNSError{Err: "server misbehaving", Name: tokenHost, Server: "198.51.100.53:53", IsTemporary: true})},
			secret:   []string{tokenHost, "198.51.100.53", "8443"},
			want:     unreached,
			code:     mcpconnectionattempts.CodeRemoteSessionUnavailable,
		},
		{
			name: "connection refused",
			tokenURL: func(t *testing.T) string {
				t.Helper()
				down := httptest.NewServer(http.NotFoundHandler())
				down.Close()
				return down.URL + "/oauth/token"
			},
			want: unreached,
			code: mcpconnectionattempts.CodeRemoteSessionUnavailable,
		},
		{
			name:     "unreachable token URL the caller set",
			tokenURL: func(*testing.T) string { return "https://" + tokenHost + "/unused" },
			headers:  map[string]string{"Mcp-Machine-Token-Url": "https://" + tokenHost + ":8443/oauth/token"},
			options:  []func(*guardian.Policy){resolving(&net.DNSError{Err: "server misbehaving", Name: tokenHost, Server: "198.51.100.53:53", IsTemporary: true})},
			secret:   []string{tokenHost, "198.51.100.53", "8443"},
			logged:   tokenHost,
			want:     unreached,
			code:     mcpconnectionattempts.CodeRemoteSessionUnavailable,
		},
		{
			name: "token service refuses the client",
			tokenURL: func(t *testing.T) string {
				t.Helper()
				return answering(t, http.StatusUnauthorized, `{"error":"invalid_client","error_description":"client secret-detail is unknown"}`)
			},
			secret: []string{"invalid_client", "secret-detail"},
			logged: "secret-detail",
			want:   refused,
			code:   mcpconnectionattempts.CodeSharedCredentialRejected,
		},
		{
			name: "token service rejects the request",
			tokenURL: func(t *testing.T) string {
				t.Helper()
				return answering(t, http.StatusBadRequest, `{"error":"invalid_scope","error_description":"scope-detail"}`)
			},
			secret: []string{"invalid_scope", "scope-detail"},
			logged: "scope-detail",
			want:   refused,
			code:   mcpconnectionattempts.CodeSharedCredentialRejected,
		},
		{
			name: "token service refuses the caller's client",
			tokenURL: func(t *testing.T) string {
				t.Helper()
				return answering(t, http.StatusUnauthorized, `{"error":"invalid_client","error_description":"client secret-detail is unknown"}`)
			},
			headers: callerClient,
			secret:  []string{"invalid_client", "secret-detail", "caller-secret"},
			logged:  "secret-detail",
			want:    refused,
		},
		{
			name: "token URL that does not exist",
			tokenURL: func(t *testing.T) string {
				t.Helper()
				return answering(t, http.StatusNotFound, `missing-detail`)
			},
			secret: []string{"missing-detail"},
			logged: "missing-detail",
			want:   unusable,
			code:   mcpconnectionattempts.CodeCredentialMisconfigured,
		},
		{
			name: "token service error",
			tokenURL: func(t *testing.T) string {
				t.Helper()
				return answering(t, http.StatusInternalServerError, `stack-detail at 10.1.2.3:5432`)
			},
			secret: []string{"stack-detail", "10.1.2.3", "5432"},
			logged: "stack-detail",
			want:   unreached,
			code:   mcpconnectionattempts.CodeRemoteSessionUnavailable,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tokenURL := tc.tokenURL(t)
			parsed, err := url.Parse(tokenURL)
			require.NoError(t, err)
			call := callClientCredentialsTool(t, tokenURL, toolServer(t).URL, tc.headers, tc.options...)

			require.True(t, call.isError)
			require.Equal(t, tc.want, toolErrorMessage(t, call.text))
			for _, surface := range []string{call.text, call.stored} {
				for _, detail := range append([]string{tokenURL, parsed.Host, parsed.Hostname(), "oauth/token", "failed to make", "dial tcp", "machine-secret"}, tc.secret...) {
					require.NotContains(t, surface, detail)
				}
			}
			require.NotRegexp(t, networkDetail, call.text)
			require.Contains(t, call.stored, tc.want)

			if tc.code == (mcpconnectionattempts.Code{}) {
				call.rec.RequireNothing()
			} else {
				row := call.rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.code)
				require.Equal(t, tc.code.String(), row.Reason)
				require.Len(t, call.rec.Rows(), 1)
			}

			require.Contains(t, call.logs, "could not process client credentials")
			require.Contains(t, call.logs, "failed to make client credentials token request")
			logged := tc.logged
			if logged == "" {
				logged = parsed.Hostname()
			}
			require.Contains(t, call.logs, logged)
		})
	}
}

// A token request that works still sends the token to the tool's server and returns its answer.
func TestClientGrantSuccessUnchanged(t *testing.T) {
	t.Parallel()

	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"machine-token","expires_in":60}`))
	}))
	t.Cleanup(token.Close)

	call := callClientCredentialsTool(t, token.URL+"/oauth/token", toolServer(t).URL, nil)
	require.False(t, call.isError)
	require.JSONEq(t, `{"authorization":"Bearer machine-token"}`, call.text)
	call.rec.RequireNothing()
}
