package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

const (
	testHealthServerID = "0b6a8f6e-6f0e-4d57-9d0c-3f1d2c0b9a11"
	testHealthIssuerID = "5d1f1a0e-2c3b-4a59-8e7f-1a2b3c4d5e6f"
	testHealthClientID = "9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4"
	testHealthRemoteID = "3c2b1a09-8f7e-4d6c-b5a4-938271605f4e"
	testHealthOtherID  = "7a6b5c4d-3e2f-4a1b-8c9d-0e1f2a3b4c5d"
)

type recordingServerHealthReader struct {
	*recordingProjectReader
	healthInput *gen.DescribeMcpServerHealthPayload
	health      *gen.AdminMcpServerHealth
	healthErr   error
	callsInput  *gen.GetMcpServerToolCallsPayload
	calls       *gen.AdminMcpServerToolCalls
	callsErr    error
}

func (r *recordingServerHealthReader) DescribeMcpServerHealth(_ context.Context, input *gen.DescribeMcpServerHealthPayload) (*gen.AdminMcpServerHealth, error) {
	r.healthInput = input
	return r.health, r.healthErr
}

func (r *recordingServerHealthReader) GetMcpServerToolCalls(_ context.Context, input *gen.GetMcpServerToolCallsPayload) (*gen.AdminMcpServerToolCalls, error) {
	r.callsInput = input
	return r.calls, r.callsErr
}

func testServerHealth() *gen.AdminMcpServerHealth {
	return &gen.AdminMcpServerHealth{
		Server:      &gen.AdminMcpServerHealthServer{ID: testHealthServerID, Name: "Example", Source: "remote", Visibility: "public", CreatedAt: "2026-01-01T00:00:00Z"},
		Correlation: &gen.AdminMcpServerHealthCorrelation{URLSlug: new("example"), McpServerID: new(testHealthServerID), ToolsetSlug: new("example-toolset")},
		UserSessionIssuer: &gen.AdminMcpServerHealthUserSessionIssuer{
			ID: testHealthIssuerID, Slug: "example-issuer", Classification: "custom", AuthnChallengeMode: "interactive",
			SessionDurationHours: 24, AttachmentScope: "project", ClientIDMetadataAdmissionMode: new("presets"),
			UseAuthenticationHost:   true,
			TrustedRemoteSession:    &gen.AdminMcpServerHealthTrustedRemoteSession{IssuerID: testHealthRemoteID, ClientID: "trusted-client"},
			OtherServersUsingIssuer: []*gen.AdminMcpServerHealthServerRef{{ID: testHealthOtherID, Name: "Other"}},
			CreatedAt:               "2026-01-02T00:00:00Z",
			Sessions: &gen.AdminMcpServerHealthUserSessions{
				DistinctSubjectsEver: 12, DistinctSubjectsInWindow: 4, FirstIssuedAt: new("2026-01-03T00:00:00Z"),
				LastIssuedAt: new("2026-09-01T12:30:00.123Z"), Live: 3,
			},
			RemoteSessionClients: []*gen.AdminMcpServerHealthRemoteSessionClient{
				testHealthClient(testHealthClientID, "dcr"),
				testHealthClient(testHealthOtherID, "cimd"),
			},
		},
		ResourceScopes: &gen.AdminMcpServerResourceScopes{
			ResourceURL: "https://mcp.example.test/mcp", PinnedScopes: []string{"read"}, AdvertisedScopesKnown: true,
			AdvertisedScopes: []string{"read", "write"}, ChallengeScopes: []string{}, SharedServerCount: 1,
			Clients: []*gen.AdminMcpServerResourceScopeClient{{
				ClientID: testHealthClientID, ScopeSource: "resource_pin", RequestedScopes: []string{"read"},
				UnadvertisedPinnedScopes: []string{}, PinWouldDecide: true,
			}},
		},
	}
}

func testToolCalls() *gen.AdminMcpServerToolCalls {
	return &gen.AdminMcpServerToolCalls{
		Type: "logging:enabled", WindowDays: new(14), Watermark: new("2026-09-28T00:00:00Z"),
		Outcomes:      &gen.AdminMcpServerToolCallOutcomes{Success: 90, Unauthorized: 5, ClientError: 2, ServerError: 1, Blocked: 1, Failed: 1, Unknown: 0},
		BucketSeconds: new(int64(86400)),
		Daily:         []*gen.AdminMcpServerToolCallBucket{{BucketStart: "2026-09-27T00:00:00Z", Total: 10, Failed: 1}},
	}
}

func testHealthClient(id, registration string) *gen.AdminMcpServerHealthRemoteSessionClient {
	return &gen.AdminMcpServerHealthRemoteSessionClient{
		ID: id, Registration: registration, TokenEndpointAuthMethod: new("client_secret_basic"),
		Scope: []string{"openid", "read"}, GrantTypes: []string{"authorization_code", "refresh_token"},
		AttachmentScope: "organization", UpstreamRejectedAt: new("2026-09-20T00:00:00Z"),
		Issuer: &gen.AdminMcpServerHealthRemoteSessionIssuer{
			ID: testHealthRemoteID, Slug: "upstream", Name: new("Upstream"), Issuer: "https://idp.example.test",
			AttachmentScope: "global", Networking: "public", Oidc: true, Pkce: "supported", CimdSupported: true,
			ScopeOverride: []string{"read"}, OmitScopeFallback: new(true), MetadataFetchedAt: new("2026-09-01T00:00:00Z"),
			MetadataLastErrorAt: new("2026-08-01T00:00:00Z"), JwksLastErrorAt: new("2026-08-02T00:00:00Z"),
		},
		Sessions: &gen.AdminMcpServerHealthRemoteSessions{
			LinkedSubjects: 3, Reauthorizations: 2, FirstLinkedAt: new("2026-02-01T00:00:00Z"),
			ValidationStatusCounts: map[string]int64{"valid": 2, "rejected_by_member": 1},
		},
	}
}

func testServerHealthReads() *recordingServerHealthReader {
	projectReads := testProjectReads()
	projectReads.project = &gen.AdminProjectDetail{ID: testProjectID, OrganizationID: "org-a"}
	return &recordingServerHealthReader{recordingProjectReader: projectReads, health: testServerHealth(), calls: testToolCalls()}
}

func callServerHealthTool(t *testing.T, reads OrganizationReader, alter func(*Principal), args string) (string, json.RawMessage, bool) {
	t.Helper()
	principal := staffPrincipal()
	principal.staff = &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: principal.Email}
	if alter != nil {
		alter(&principal)
	}
	handler := NewRuntime(&testAuthenticator{principal: principal}, "", reads).Handler()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"describe_mcp_server_health","arguments":`+args+`}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var message struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
			IsError           bool            `json:"isError"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
	return response.Body.String(), message.Result.StructuredContent, message.Result.IsError
}

func healthArgs(window string) string {
	args := `{"organization_id":"org-a","project_id":"` + testProjectID + `","mcp_server_id":"` + testHealthServerID + `"`
	if window != "" {
		args += `,"window_days":` + window
	}
	return args + `}`
}

func TestDescribeMCPServerHealthExactTargetAndProjection(t *testing.T) {
	t.Parallel()
	reads := testServerHealthReads()
	body, data, isError := callServerHealthTool(t, reads, nil, healthArgs(""))
	require.False(t, isError, body)
	require.Equal(t, &gen.GetProjectPayload{IDOrSlug: testProjectID, OrganizationIDOrSlug: new("org-a")}, reads.getInput)
	require.Equal(t, &gen.DescribeMcpServerHealthPayload{OrganizationID: "org-a", ProjectID: testProjectID, McpServerID: testHealthServerID, WindowDays: 14}, reads.healthInput)
	require.Equal(t, &gen.GetMcpServerToolCallsPayload{OrganizationID: "org-a", ProjectID: testProjectID, McpServerID: testHealthServerID, WindowDays: 14}, reads.callsInput)

	var output MCPServerHealth
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, "org-a", output.OrganizationID)
	require.Equal(t, testProjectID, output.ProjectID)
	require.Equal(t, MCPServerHealthServer{ID: testHealthServerID, Name: "Example", Source: "remote", Visibility: "public", CreatedAt: "2026-01-01T00:00:00Z"}, output.Server)
	require.Equal(t, "example", *output.Correlation.URLSlug)
	require.Nil(t, output.LegacyAuth)
	require.NotNil(t, output.UserSessionIssuer)
	require.Len(t, output.UserSessionIssuer.RemoteSessionClients, 2)
	client := output.UserSessionIssuer.RemoteSessionClients[0]
	require.Equal(t, MCPServerHealthValidationCounts{Valid: 2, RejectedByMember: 1}, client.Sessions.ValidationStatusCounts)
	require.Equal(t, "https://idp.example.test", client.Issuer.Issuer)
	require.Equal(t, "2026-08-01T00:00:00Z", *client.Issuer.MetadataLastErrorAt)
	require.Equal(t, MCPServerHealthOutcomes{Success: 90, Unauthorized: 5, ClientError: 2, ServerError: 1, Blocked: 1, Failed: 1}, *output.ToolCalls.Outcomes)
	require.Equal(t, &MCPServerHealthResourceScopes{
		ResourceURL: "https://mcp.example.test/mcp", PinnedScopes: []string{"read"}, AdvertisedScopesKnown: true,
		AdvertisedScopes: []string{"read", "write"}, ChallengeScopes: []string{}, SharedServerCount: 1,
		Clients: []MCPServerHealthResourceScopeClient{{
			ClientID: testHealthClientID, ScopeSource: "resource_pin", RequestedScopes: []string{"read"},
			UnadvertisedPinnedScopes: []string{}, PinWouldDecide: true,
		}},
	}, output.ResourceScopes)

	// The daily series and bucket width are service-only.
	require.NotContains(t, string(data), "daily")
	require.NotContains(t, string(data), "bucket")
}

func TestDescribeMCPServerHealthWindowDays(t *testing.T) {
	t.Parallel()
	reads := testServerHealthReads()
	reads.calls.WindowDays = new(90)
	body, _, isError := callServerHealthTool(t, reads, nil, healthArgs("90"))
	require.False(t, isError, body)
	require.Equal(t, 90, reads.healthInput.WindowDays)
	require.Equal(t, 90, reads.callsInput.WindowDays)

	for _, window := range []string{"7", "-14", "31"} {
		reads := testServerHealthReads()
		body, _, isError := callServerHealthTool(t, reads, nil, healthArgs(window))
		require.True(t, isError, window)
		require.Contains(t, body, "window_days must be 14, 30 or 90")
		require.Nil(t, reads.healthInput)
	}

	// A service window that disagrees with the request is rejected.
	reads = testServerHealthReads()
	body, _, isError = callServerHealthTool(t, reads, nil, healthArgs("30"))
	require.True(t, isError)
	require.Contains(t, body, errServerHealthUnavailable.Error())
}

func TestDescribeMCPServerHealthLegacyAndLoggingDisabled(t *testing.T) {
	t.Parallel()
	reads := testServerHealthReads()
	reads.health.UserSessionIssuer = nil
	reads.health.LegacyAuth = new("oauth_proxy")
	reads.calls = &gen.AdminMcpServerToolCalls{Type: "logging:disabled"}
	body, data, isError := callServerHealthTool(t, reads, nil, healthArgs(""))
	require.False(t, isError, body)
	var output MCPServerHealth
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, "oauth_proxy", *output.LegacyAuth)
	require.Nil(t, output.UserSessionIssuer)
	require.Equal(t, MCPServerHealthToolCalls{Type: "logging:disabled"}, output.ToolCalls)
}

func TestDescribeMCPServerHealthWithoutURLSlug(t *testing.T) {
	t.Parallel()
	reads := testServerHealthReads()
	reads.health.Correlation.URLSlug = nil
	body, data, isError := callServerHealthTool(t, reads, nil, healthArgs(""))
	require.False(t, isError, body)
	var output MCPServerHealth
	require.NoError(t, json.Unmarshal(data, &output))
	require.Nil(t, output.Correlation.URLSlug)
	require.NotContains(t, string(data), "url_slug")
}

func TestDescribeMCPServerHealthToolsetOnlyCorrelation(t *testing.T) {
	t.Parallel()
	reads := testServerHealthReads()
	reads.health.Server.Source = "toolset_only"
	reads.health.Correlation.McpServerID = nil
	reads.health.ResourceScopes = nil
	body, data, isError := callServerHealthTool(t, reads, nil, healthArgs(""))
	require.False(t, isError, body)
	var output MCPServerHealth
	require.NoError(t, json.Unmarshal(data, &output))
	require.Nil(t, output.Correlation.MCPServerID)
	require.Equal(t, "example-toolset", *output.Correlation.ToolsetSlug)
}

func TestDescribeMCPServerHealthRejectsInexactTargets(t *testing.T) {
	t.Parallel()
	for name, args := range map[string]string{
		"project slug":  `{"organization_id":"org-a","project_id":"example","mcp_server_id":"` + testHealthServerID + `"}`,
		"padded server": `{"organization_id":"org-a","project_id":"` + testProjectID + `","mcp_server_id":" ` + testHealthServerID + `"}`,
		"missing org":   `{"project_id":"` + testProjectID + `","mcp_server_id":"` + testHealthServerID + `"}`,
	} {
		reads := testServerHealthReads()
		_, _, isError := callServerHealthTool(t, reads, nil, args)
		require.True(t, isError, name)
		require.Nil(t, reads.healthInput, name)
	}

	// A project in another organization never reaches the health read.
	reads := testServerHealthReads()
	reads.project.OrganizationID = "org-b"
	body, _, isError := callServerHealthTool(t, reads, nil, healthArgs(""))
	require.True(t, isError)
	require.Contains(t, body, errServerHealthUnavailable.Error())
	require.Nil(t, reads.healthInput)
}

func TestDescribeMCPServerHealthUnavailableDependencies(t *testing.T) {
	t.Parallel()

	// Unverified staff.
	reads := testServerHealthReads()
	body, _, isError := callServerHealthTool(t, reads, func(p *Principal) { p.staff = nil }, healthArgs(""))
	require.True(t, isError)
	require.Contains(t, body, errServerHealthUnavailable.Error())
	require.Nil(t, reads.healthInput)

	// No health reader wired: the tool fails closed.
	projectReads := testProjectReads()
	projectReads.project = &gen.AdminProjectDetail{ID: testProjectID, OrganizationID: "org-a"}
	body, _, isError = callServerHealthTool(t, projectReads, nil, healthArgs(""))
	require.True(t, isError)
	require.Contains(t, body, errServerHealthUnavailable.Error())

	// Service error text is never echoed.
	reads = testServerHealthReads()
	reads.healthErr = errors.New("clickhouse: secret-bearing failure")
	body, _, isError = callServerHealthTool(t, reads, nil, healthArgs(""))
	require.True(t, isError)
	require.Contains(t, body, errServerHealthUnavailable.Error())
	require.NotContains(t, body, "secret-bearing")

	// A tool calls failure fails the whole tool; there is no partial result.
	reads = testServerHealthReads()
	reads.callsErr = errors.New("clickhouse: secret-bearing failure")
	body, data, isError := callServerHealthTool(t, reads, nil, healthArgs(""))
	require.True(t, isError)
	require.Contains(t, body, errServerHealthUnavailable.Error())
	require.NotContains(t, body, "secret-bearing")
	require.NotContains(t, string(data), "example-issuer")
}

func TestDescribeMCPServerHealthFailsClosedOnMalformedResults(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", maxHealthValueLength+1)
	cases := map[string]func(*gen.AdminMcpServerHealth){
		"nil result":            nil,
		"nil server":            func(h *gen.AdminMcpServerHealth) { h.Server = nil },
		"other server":          func(h *gen.AdminMcpServerHealth) { h.Server.ID = testHealthOtherID },
		"unknown source":        func(h *gen.AdminMcpServerHealth) { h.Server.Source = "other" },
		"long name":             func(h *gen.AdminMcpServerHealth) { h.Server.Name = long },
		"bad created_at":        func(h *gen.AdminMcpServerHealth) { h.Server.CreatedAt = "yesterday" },
		"empty url slug":        func(h *gen.AdminMcpServerHealth) { h.Correlation.URLSlug = new("") },
		"long url slug":         func(h *gen.AdminMcpServerHealth) { h.Correlation.URLSlug = new(long) },
		"missing mcp server id": func(h *gen.AdminMcpServerHealth) { h.Correlation.McpServerID = nil },
		"other mcp server id":   func(h *gen.AdminMcpServerHealth) { h.Correlation.McpServerID = new(testHealthOtherID) },
		"toolset_only with id":  func(h *gen.AdminMcpServerHealth) { h.Server.Source = "toolset_only" },
		"empty toolset slug":    func(h *gen.AdminMcpServerHealth) { h.Correlation.ToolsetSlug = new("") },
		"nil scope":             func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.RemoteSessionClients[0].Scope = nil },
		"nil grant types":       func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.RemoteSessionClients[0].GrantTypes = nil },
		"nil validation counts": func(h *gen.AdminMcpServerHealth) {
			h.UserSessionIssuer.RemoteSessionClients[0].Sessions.ValidationStatusCounts = nil
		},
		"scopes on toolset":    func(h *gen.AdminMcpServerHealth) { h.Server.Source = "toolset" },
		"empty resource url":   func(h *gen.AdminMcpServerHealth) { h.ResourceScopes.ResourceURL = "" },
		"nil pinned scopes":    func(h *gen.AdminMcpServerHealth) { h.ResourceScopes.PinnedScopes = nil },
		"empty pinned scope":   func(h *gen.AdminMcpServerHealth) { h.ResourceScopes.PinnedScopes = []string{""} },
		"nil challenge scopes": func(h *gen.AdminMcpServerHealth) { h.ResourceScopes.ChallengeScopes = nil },
		"negative shared":      func(h *gen.AdminMcpServerHealth) { h.ResourceScopes.SharedServerCount = -1 },
		"unknown scope source": func(h *gen.AdminMcpServerHealth) { h.ResourceScopes.Clients[0].ScopeSource = "guess" },
		"inexact scope client": func(h *gen.AdminMcpServerHealth) { h.ResourceScopes.Clients[0].ClientID = "client" },
		"nil scope client": func(h *gen.AdminMcpServerHealth) {
			h.ResourceScopes.Clients = append(h.ResourceScopes.Clients, nil)
		},
		"too many pinned": func(h *gen.AdminMcpServerHealth) {
			h.ResourceScopes.PinnedScopes = make([]string, maxHealthPinnedScopes+1)
		},
		"legacy with issuer":  func(h *gen.AdminMcpServerHealth) { h.LegacyAuth = new("gram_private") },
		"unknown legacy":      func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer = nil; h.LegacyAuth = new("basic") },
		"unscoped attachment": func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.AttachmentScope = "project:" + testProjectID },
		"unknown authn mode":  func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.AuthnChallengeMode = "silent" },
		"unknown admission":   func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.ClientIDMetadataAdmissionMode = new("any") },
		"nil user sessions":   func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.Sessions = nil },
		"negative live":       func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.Sessions.Live = -1 },
		"nil other server": func(h *gen.AdminMcpServerHealth) {
			h.UserSessionIssuer.OtherServersUsingIssuer = append(h.UserSessionIssuer.OtherServersUsingIssuer, nil)
		},
		"too many other": func(h *gen.AdminMcpServerHealth) {
			h.UserSessionIssuer.OtherServersUsingIssuer = make([]*gen.AdminMcpServerHealthServerRef, maxHealthOtherServers+1)
		},
		"too many clients": func(h *gen.AdminMcpServerHealth) {
			h.UserSessionIssuer.RemoteSessionClients = make([]*gen.AdminMcpServerHealthRemoteSessionClient, maxHealthClients+1)
		},
		"nil client":           func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.RemoteSessionClients[0] = nil },
		"unknown registration": func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.RemoteSessionClients[0].Registration = "manual" },
		"unknown auth method": func(h *gen.AdminMcpServerHealth) {
			h.UserSessionIssuer.RemoteSessionClients[0].TokenEndpointAuthMethod = new("tls")
		},
		"too many scopes": func(h *gen.AdminMcpServerHealth) {
			h.UserSessionIssuer.RemoteSessionClients[0].Scope = make([]string, maxHealthScopes+1)
		},
		"nil remote issuer": func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.RemoteSessionClients[0].Issuer = nil },
		"long issuer url": func(h *gen.AdminMcpServerHealth) {
			h.UserSessionIssuer.RemoteSessionClients[0].Issuer.Issuer = "https://" + strings.Repeat("a", maxHealthIssuerURL)
		},
		"unknown pkce": func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.RemoteSessionClients[0].Issuer.Pkce = "maybe" },
		"unknown networking": func(h *gen.AdminMcpServerHealth) {
			h.UserSessionIssuer.RemoteSessionClients[0].Issuer.Networking = "vpn"
		},
		"nil remote sessions": func(h *gen.AdminMcpServerHealth) { h.UserSessionIssuer.RemoteSessionClients[0].Sessions = nil },
		"unknown status key": func(h *gen.AdminMcpServerHealth) {
			h.UserSessionIssuer.RemoteSessionClients[0].Sessions.ValidationStatusCounts["expired"] = 1
		},
		"negative status count": func(h *gen.AdminMcpServerHealth) {
			h.UserSessionIssuer.RemoteSessionClients[0].Sessions.ValidationStatusCounts["valid"] = -1
		},
	}
	for name, mutate := range cases {
		reads := testServerHealthReads()
		if mutate == nil {
			reads.health = nil
		} else {
			mutate(reads.health)
		}
		body, data, isError := callServerHealthTool(t, reads, nil, healthArgs(""))
		require.True(t, isError, name)
		require.Contains(t, body, errServerHealthUnavailable.Error(), name)
		require.NotContains(t, string(data), "example-issuer", name)
	}

	callCases := map[string]func(*gen.AdminMcpServerToolCalls){
		"nil tool calls":       nil,
		"unknown tool calls":   func(c *gen.AdminMcpServerToolCalls) { c.Type = "logging:partial" },
		"disabled with counts": func(c *gen.AdminMcpServerToolCalls) { c.Type = "logging:disabled" },
		"enabled no outcomes":  func(c *gen.AdminMcpServerToolCalls) { c.Outcomes = nil },
		"negative outcome":     func(c *gen.AdminMcpServerToolCalls) { c.Outcomes.Failed = -1 },
		"bad watermark":        func(c *gen.AdminMcpServerToolCalls) { c.Watermark = new("soon") },
		"disabled with bucket": func(c *gen.AdminMcpServerToolCalls) {
			*c = gen.AdminMcpServerToolCalls{Type: "logging:disabled", BucketSeconds: new(int64(86400))}
		},
		"disabled with daily": func(c *gen.AdminMcpServerToolCalls) {
			*c = gen.AdminMcpServerToolCalls{Type: "logging:disabled", Daily: []*gen.AdminMcpServerToolCallBucket{}}
		},
		"other window": func(c *gen.AdminMcpServerToolCalls) { c.WindowDays = new(30) },
	}
	for name, mutate := range callCases {
		reads := testServerHealthReads()
		if mutate == nil {
			reads.calls = nil
		} else {
			mutate(reads.calls)
		}
		body, data, isError := callServerHealthTool(t, reads, nil, healthArgs(""))
		require.True(t, isError, name)
		require.Contains(t, body, errServerHealthUnavailable.Error(), name)
		require.NotContains(t, string(data), "example-issuer", name)
	}
}

func TestDescribeMCPServerHealthContextAvailability(t *testing.T) {
	t.Parallel()
	const workflow = "inspect one MCP server's authentication configuration, session counts and tool call outcomes"
	_, body, _ := callStaffReadTool(t, testServerHealthReads(), "get_admin_context", `{}`)
	require.Contains(t, body, workflow)
	_, body, _ = callStaffReadTool(t, testProjectReads(), "get_admin_context", `{}`)
	require.NotContains(t, body, workflow)
}
