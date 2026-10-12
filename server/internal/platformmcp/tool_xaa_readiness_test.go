package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	srv "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/authz"
	idpc "github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

const xaaProjectID = "11111111-1111-4111-8111-111111111111"
const xaaServerID = "22222222-2222-4222-8222-222222222222"

type xaaReaderStub struct {
	result *srv.ListOktaResourceConnectionsResult
	err    error
	calls  int
}

func (s *xaaReaderStub) List(_ context.Context, payload *srv.ListPayload) (*srv.ListOktaResourceConnectionsResult, error) {
	s.calls++
	if !payload.IncludeAll {
		return nil, errors.New("readiness must include non-pending states")
	}
	return s.result, s.err
}

func xaaTestSession(t *testing.T, service *xaaReadinessService, deny bool) *mcp.ClientSession {
	t.Helper()
	server := newTestMCPServer()
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	if deny {
		reg.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{RequiredScope: "org:admin", cause: ErrForbidden}})
	} else {
		reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	}
	registerXAAReadinessTool(reg, service)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "xaa-test", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func xaaCall(t *testing.T, session *mcp.ClientSession, projectID, serverID string) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_xaa_readiness", Arguments: map[string]any{"project_id": projectID, "mcp_server_id": serverID}})
	require.NoError(t, err)
	return result
}

func TestXAAReadinessContract(t *testing.T) {
	t.Parallel()
	live, unavailable := newRegistrar(newTestMCPServer()), newRegistrar(newTestMCPServer())
	registerXAAReadinessTool(live, &xaaReadinessService{})
	registerXAAReadinessTool(unavailable, nil)
	a, b := live.Descriptors()[0], unavailable.Descriptors()[0]
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.output, b.output)
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, ExternalAuthorizationOrgAdmin, a.Meta.Authorization)
	require.Equal(t, discoveryMCPRead, a.Meta.DiscoveryScopes)
	require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
	require.Equal(t, externalOnly, a.Meta.Audiences)
	require.True(t, a.Annotations.ReadOnlyHint)
	require.Empty(t, live.For(AudienceAssistant))
	var schema struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(a.InputSchema, &schema))
	require.ElementsMatch(t, []string{"project_id", "mcp_server_id"}, schema.Required)
	principal := testPrincipal()
	tools := []*mcp.Tool{{Name: a.Name}}
	admin := authz.GrantsToContext(t.Context(), []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)})
	require.Empty(t, live.FilterExternalTools(admin, principal, tools))
	both := authz.GrantsToContext(t.Context(), []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID), authz.NewGrant(authz.ScopeMCPRead, xaaServerID)})
	require.Len(t, live.FilterExternalTools(both, principal, tools), 1)
}

func TestXAAReadinessProjectsExistingStatesWithoutLeakingSnapshot(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"not_applicable", "needs_agent", "needs_connection", "broken", "connected", "verified"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			observation, at, reason := "success", "2026-01-01T00:00:00Z", "downstream_rejected"
			reader := &xaaReaderStub{result: &srv.ListOktaResourceConnectionsResult{TotalCount: 99, Servers: []*srv.OktaResourceConnectionServer{
				{ProjectID: xaaProjectID, McpServerID: "hidden-server", State: "hidden-state"},
				{ProjectID: xaaProjectID, McpServerID: xaaServerID, State: state, Pending: true, BrokenReason: &reason, ObservedResult: &observation, ObservedAt: &at, ServerName: "private-label", ResourceIndicator: "https://private.invalid", ClientID: &observation},
			}}}
			session := xaaTestSession(t, &xaaReadinessService{connections: reader, enabled: func(context.Context, string) (bool, error) { return true, nil }}, false)
			result := xaaCall(t, session, xaaProjectID, xaaServerID)
			require.False(t, result.IsError)
			var output GetXAAReadinessOutput
			wire, err := json.Marshal(result.StructuredContent)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(wire, &output))
			require.Equal(t, state, output.State)
			require.Equal(t, xaaProjectID, output.ProjectID)
			require.Equal(t, xaaServerID, output.MCPServerID)
			require.True(t, output.Pending)
			require.Equal(t, &observation, output.ObservedResult)
			require.Equal(t, &at, output.ObservedAt)
			require.Equal(t, &reason, output.BrokenReason)
			for _, secret := range []string{"hidden", "private", "total_count", "client_id", "servers"} {
				require.NotContains(t, string(wire), secret)
			}
			require.Equal(t, 1, reader.calls)
		})
	}
}

func TestXAAReadinessRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, code            string
		enabled, deny, absent bool
		flagErr, serviceErr   error
		project, server       string
		calls                 int
	}{
		{name: "unavailable", code: unavailableCode, absent: true},
		{name: "disabled", code: unavailableCode},
		{name: "flag failure", code: unavailableCode, flagErr: errors.New("private flag detail")},
		{name: "org admin denial", code: "permission_denied", enabled: true, deny: true},
		{name: "service denial", code: "forbidden", enabled: true, serviceErr: oops.C(oops.CodeForbidden), calls: 1},
		{name: "no identity provider", code: "setup_required", enabled: true, serviceErr: oops.C(oops.CodeFailedPrecondition), calls: 1},
		{name: "service failure", code: unavailableCode, enabled: true, serviceErr: errors.New("private database detail"), calls: 1},
		{name: "hidden or missing", code: "not_found", enabled: true, calls: 1},
		{name: "wrong project", code: "not_found", enabled: true, project: "33333333-3333-4333-8333-333333333333", calls: 1},
		{name: "invalid project", code: "invalid_request", project: "not-a-uuid"},
		{name: "invalid server", code: "invalid_request", server: "not-a-uuid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := &xaaReaderStub{err: tc.serviceErr, result: &srv.ListOktaResourceConnectionsResult{Servers: []*srv.OktaResourceConnectionServer{{ProjectID: xaaProjectID, McpServerID: "44444444-4444-4444-8444-444444444444", State: "verified"}}}}
			signIn := &signInReaderStub{}
			service := &xaaReadinessService{connections: reader, signIn: signIn, enabled: func(context.Context, string) (bool, error) { return tc.enabled, tc.flagErr }}
			if tc.absent {
				service = nil
			}
			if tc.name == "wrong project" {
				reader.result.Servers[0].McpServerID = xaaServerID
			}
			session := xaaTestSession(t, service, tc.deny)
			project, server := tc.project, tc.server
			if project == "" {
				project = xaaProjectID
			}
			if server == "" {
				server = xaaServerID
			}
			result := xaaCall(t, session, project, server)
			require.True(t, result.IsError)
			require.Len(t, result.Content, 1)
			content, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			text := content.Text
			require.Contains(t, text, `"code":"`+tc.code+`"`)
			require.NotContains(t, text, "private")
			require.Equal(t, tc.calls, reader.calls)
			require.Empty(t, signIn.orgs, "a refusal never reads sign-in setup")
		})
	}
}

type xaaIdentityStub struct {
	chaining XAAIdentityChaining
	callback *string
	err      error
	issuer   uuid.UUID
	resource string
	calls    int
}

func (s *xaaIdentityStub) Inspect(_ context.Context, _ string, _, _, issuer uuid.UUID, resource string) (XAAIdentityChaining, *string, error) {
	s.calls++
	s.issuer, s.resource = issuer, resource
	return s.chaining, s.callback, s.err
}

func TestXAAReadinessReportsIdentityChainingAndFederatedCallback(t *testing.T) {
	t.Parallel()
	issuer := uuid.New()
	issuerID := issuer.String()
	callback := "https://gram.example.test/oauth/idp_callback/" + uuid.NewString()
	reader := &xaaReaderStub{result: &srv.ListOktaResourceConnectionsResult{Servers: []*srv.OktaResourceConnectionServer{
		{ProjectID: xaaProjectID, McpServerID: xaaServerID, State: "connected", IssuerID: &issuerID, ResourceIndicator: "https://upstream.example.test/mcp"},
	}}}
	identity := &xaaIdentityStub{callback: &callback, chaining: XAAIdentityChaining{Served: false, Bindings: []XAAIdentityChainingBinding{
		{State: "unknown_grants", Stage: "registration", Remediation: "An administrator must confirm effective registration grants.", GrantSource: "unknown", RequestedScopes: []string{"read"}},
	}}}
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	session := xaaTestSession(t, &xaaReadinessService{connections: reader, enabled: enabled, identity: identity}, false)
	result := xaaCall(t, session, xaaProjectID, xaaServerID)
	require.False(t, result.IsError)
	var output GetXAAReadinessOutput
	wire, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(wire, &output))
	require.Equal(t, issuer, identity.issuer)
	require.Equal(t, "https://upstream.example.test/mcp", identity.resource)
	require.NotNil(t, output.IdentityChaining)
	require.Equal(t, identity.chaining, *output.IdentityChaining)
	require.Equal(t, &callback, output.FederatedCallbackURL)
	require.NotContains(t, string(wire), "upstream.example.test", "the resource indicator stays internal")

	identity.err = errors.New("private database detail")
	failed := xaaCall(t, session, xaaProjectID, xaaServerID)
	require.True(t, failed.IsError)
	content, ok := failed.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, content.Text, `"code":"`+unavailableCode+`"`)
	require.NotContains(t, content.Text, "private")

	plain := xaaCall(t, xaaTestSession(t, &xaaReadinessService{connections: reader, enabled: enabled}, false), xaaProjectID, xaaServerID)
	require.False(t, plain.IsError)
	wire, err = json.Marshal(plain.StructuredContent)
	require.NoError(t, err)
	require.NotContains(t, string(wire), "identity_chaining")
	require.NotContains(t, string(wire), "federated_callback_url")
}

type signInReaderStub struct {
	setup *idpc.SignInSetup
	err   error
	orgs  []string
}

func (s *signInReaderStub) Read(_ context.Context, organizationID string) (*idpc.SignInSetup, error) {
	s.orgs = append(s.orgs, organizationID)
	return s.setup, s.err
}

func xaaSignInCall(t *testing.T, signIn *signInReaderStub) (GetXAAReadinessOutput, string) {
	t.Helper()
	return xaaSignInCallLogged(t, signIn, nil)
}

func xaaSignInCallLogged(t *testing.T, signIn *signInReaderStub, logger *slog.Logger) (GetXAAReadinessOutput, string) {
	t.Helper()
	reader := &xaaReaderStub{result: &srv.ListOktaResourceConnectionsResult{Servers: []*srv.OktaResourceConnectionServer{{ProjectID: xaaProjectID, McpServerID: xaaServerID, State: "needs_connection"}}}}
	session := xaaTestSession(t, &xaaReadinessService{logger: logger, connections: reader, signIn: signIn, enabled: func(context.Context, string) (bool, error) { return true, nil }}, false)
	result := xaaCall(t, session, xaaProjectID, xaaServerID)
	require.False(t, result.IsError)
	wire, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var output GetXAAReadinessOutput
	require.NoError(t, json.Unmarshal(wire, &output))
	require.Equal(t, "needs_connection", output.State)
	return output, string(wire)
}

func TestXAAReadinessReportsOktaSignInSetup(t *testing.T) {
	t.Parallel()
	done, notDone := true, false
	signIn := &signInReaderStub{setup: &idpc.SignInSetup{
		ConnectionStatus:     idpc.StatusVerified,
		AgentRecorded:        true,
		ClientRegistered:     true,
		ClientReady:          true,
		RedirectURI:          "https://app.example.com/mcp/idp_callback/client",
		JWKSURL:              "https://app.example.com/.well-known/oauth-client/client/jwks.json",
		ActiveKeyID:          "kid-1",
		PublicJWK:            map[string]any{"kid": "kid-1", "kty": "RSA", "alg": "RS256", "use": "sig", "n": "modulus", "e": "AQAB"},
		TrustingIssuers:      []idpc.SignInIssuer{{ID: "issuer-id", Slug: "okta-sign-in"}},
		StaleTrustingIssuers: []idpc.SignInIssuer{{ID: "stale-id", Slug: "old-agent"}},
		Checklist: []idpc.ChecklistItem{
			{Key: idpc.ChecklistKeySubmitClientID, Title: "Submit", Completed: &done, Description: "private copy"},
			{Key: idpc.ChecklistKeyAddAgentPublicKey, Title: "Key", Completed: nil},
			{Key: idpc.ChecklistKeyActivateAgentApp, Title: "Activate", Completed: &notDone},
			{Key: idpc.ChecklistKeyFirstResourceConnection, Title: "First", Completed: nil},
		},
		NextStep: idpc.SignInStepRegisterInOkta,
	}}
	output, wire := xaaSignInCall(t, signIn)
	require.Equal(t, []string{testPrincipal().OrganizationID}, signIn.orgs)
	got := output.OktaSignIn
	require.NotNil(t, got)
	require.Equal(t, idpc.SignInStepRegisterInOkta, got.NextStep)
	for _, want := range []string{"paste public_jwk in the agent's Credentials and click Activate", "redirect_uri", "pasted in Okta again"} {
		require.Contains(t, got.NextStepGuidance, want)
	}
	require.Equal(t, "kid-1", *got.KeyID)
	require.Equal(t, map[string]any{"kid": "kid-1", "kty": "RSA", "alg": "RS256", "use": "sig", "n": "modulus", "e": "AQAB"}, got.PublicJWK)
	require.Equal(t, []OktaSignInIssuer{{ID: "stale-id", Slug: "old-agent"}}, got.StaleTrustingIssuers)
	require.True(t, got.AgentRecorded)
	require.True(t, got.ClientRegistered)
	require.True(t, got.ClientReady)
	require.Equal(t, "https://app.example.com/mcp/idp_callback/client", *got.RedirectURI)
	require.Equal(t, "https://app.example.com/.well-known/oauth-client/client/jwks.json", *got.KeyURL)
	require.Equal(t, []OktaSignInIssuer{{ID: "issuer-id", Slug: "okta-sign-in"}}, got.TrustingIssuers)
	require.Equal(t, &OktaChecklistStep{Key: idpc.ChecklistKeyActivateAgentApp, Title: "Activate", Completed: &notDone}, got.ChecklistNextStep, "unobservable steps are skipped once Speakeasy's side is done")
	require.NotContains(t, wire, "private copy")
}

func TestXAAReadinessOktaSignInChecklistSkipsOnlyUnobservableSteps(t *testing.T) {
	t.Parallel()
	done := true
	checklist := []idpc.ChecklistItem{
		{Key: idpc.ChecklistKeySubmitClientID, Title: "Submit", Completed: &done},
		{Key: idpc.ChecklistKeyAddAgentPublicKey, Title: "Key", Completed: nil},
		{Key: idpc.ChecklistKeyFirstResourceConnection, Title: "First", Completed: nil},
	}
	for _, tc := range []struct {
		step string
		want *OktaChecklistStep
	}{
		{step: idpc.SignInStepRegisterInOkta, want: nil},
		{step: idpc.SignInStepTrustSignIn, want: &OktaChecklistStep{Key: idpc.ChecklistKeyAddAgentPublicKey, Title: "Key", Completed: nil}},
	} {
		t.Run(tc.step, func(t *testing.T) {
			t.Parallel()
			output, _ := xaaSignInCall(t, &signInReaderStub{setup: &idpc.SignInSetup{ConnectionStatus: idpc.StatusVerified, TrustingIssuers: []idpc.SignInIssuer{}, Checklist: checklist, NextStep: tc.step}})
			require.NotNil(t, output.OktaSignIn)
			require.Equal(t, tc.want, output.OktaSignIn.ChecklistNextStep)
		})
	}
}

func TestXAAReadinessOktaSignInReportsDuplicateClients(t *testing.T) {
	t.Parallel()
	output, wire := xaaSignInCall(t, &signInReaderStub{setup: &idpc.SignInSetup{
		ConnectionStatus: idpc.StatusVerified, AgentRecorded: true, ClientRegistered: true, DuplicateClients: 2,
		TrustingIssuers: []idpc.SignInIssuer{}, NextStep: idpc.SignInStepResolveDuplicates,
	}})
	got := output.OktaSignIn
	require.NotNil(t, got)
	require.Equal(t, idpc.SignInStepResolveDuplicates, got.NextStep)
	require.Equal(t, 2, got.DuplicateClients)
	require.Contains(t, got.NextStepGuidance, "Delete the extras")
	require.Nil(t, got.PublicJWK)
	require.NotContains(t, wire, "redirect_uri")
}

func TestXAAReadinessLogsFailedOktaSignInRead(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err    error
		logged bool
	}{
		"read failure":  {err: errors.New("database unavailable"), logged: true},
		"no connection": {err: idpc.ErrConnectionNotFound, logged: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			output, _ := xaaSignInCallLogged(t, &signInReaderStub{err: tc.err}, slog.New(slog.NewTextHandler(&logs, nil)))
			require.Nil(t, output.OktaSignIn)
			require.Equal(t, tc.logged, strings.Contains(logs.String(), "database unavailable"))
		})
	}
}

func TestXAAReadinessOktaSignInBeforeSetup(t *testing.T) {
	t.Parallel()
	signIn := &signInReaderStub{setup: &idpc.SignInSetup{ConnectionStatus: idpc.StatusVerified, TrustingIssuers: []idpc.SignInIssuer{}, Checklist: []idpc.ChecklistItem{{Key: idpc.ChecklistKeyRegisterAIAgent, Title: "Register"}}, NextStep: idpc.SignInStepRecordAgent}}
	output, wire := xaaSignInCall(t, signIn)
	got := output.OktaSignIn
	require.NotNil(t, got)
	require.Equal(t, idpc.SignInStepRecordAgent, got.NextStep)
	require.Contains(t, got.NextStepGuidance, "Platform MCP cannot")
	require.Nil(t, got.RedirectURI)
	require.Nil(t, got.KeyURL)
	require.NotContains(t, wire, "redirect_uri")
	require.Equal(t, idpc.ChecklistKeyRegisterAIAgent, got.ChecklistNextStep.Key)
	require.Nil(t, got.ChecklistNextStep.Completed, "an unobservable step reads as not confirmed")
}

func TestXAAReadinessOmitsOktaSignInWhenUnreadable(t *testing.T) {
	t.Parallel()
	for name, signIn := range map[string]*signInReaderStub{
		"read failure":  {err: errors.New("private database detail")},
		"no connection": {err: idpc.ErrConnectionNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			output, wire := xaaSignInCall(t, signIn)
			require.Nil(t, output.OktaSignIn)
			require.NotContains(t, wire, "private")
		})
	}
}

func TestXAAReadinessOktaSignInGuidanceCoversEveryStep(t *testing.T) {
	t.Parallel()
	dashboardSteps := []string{idpc.SignInStepVerifyConnection, idpc.SignInStepRecordAgent, idpc.SignInStepResolveDuplicates, idpc.SignInStepSetUpSignIn, idpc.SignInStepTrustSignIn}
	for _, step := range append(dashboardSteps, idpc.SignInStepAwaitProvision, idpc.SignInStepRegisterInOkta) {
		require.NotEmpty(t, oktaSignInGuidance[step], step)
	}
	for _, step := range dashboardSteps {
		require.Contains(t, oktaSignInGuidance[step], "Platform MCP cannot make this change", step)
	}
	require.Contains(t, oktaSignInGuidance[idpc.SignInStepSetUpSignIn], "customer-managed encryption keys")
	require.Contains(t, oktaSignInGuidance[idpc.SignInStepSetUpSignIn], "Google Cloud KMS key")
	descriptor := func() Descriptor {
		reg := newRegistrar(newTestMCPServer())
		registerXAAReadinessTool(reg, nil)
		return reg.Descriptors()[0]
	}()
	require.Contains(t, descriptor.Description, "send the administrator to the dashboard")
}
