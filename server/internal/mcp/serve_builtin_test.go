package mcp_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	builtinrepo "github.com/speakeasy-api/gram/server/internal/builtinmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func seedBuiltinAgent(t *testing.T, ctx context.Context, ti *testInstance, explicit bool) (uuid.UUID, string) {
	t.Helper()
	ac, _ := contextvalues.GetAuthContext(ctx)
	// Mirror project creation: every ordinary project starts with default.
	_, envErr := envrepo.New(ti.conn).GetEnvironmentBySlug(ctx, envrepo.GetEnvironmentBySlugParams{Slug: "default", ProjectID: *ac.ProjectID})
	if errors.Is(envErr, pgx.ErrNoRows) {
		_, envErr = envrepo.New(ti.conn).CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Default", Slug: "default"})
	}
	require.NoError(t, envErr)
	id := builtinmcp.ID(*ac.ProjectID, builtinmcp.Slack)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: ac.ActiveOrganizationID, OwnerUserID: ac.UserID, Name: "Built-in test agent " + uuid.NewString()})
	require.NoError(t, err)
	selector := authz.NewSelector(authz.ScopeMCPConnect, id.String())
	selector[authz.SelectorKeyProjectID] = ac.ProjectID.String()
	if !explicit {
		selector[authz.SelectorKeyResourceID] = "*"
	}
	encoded, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), Scope: string(authz.ScopeMCPConnect), Selectors: encoded})
	require.NoError(t, err)
	seedUserMCPConnectGrant(t, ctx, ti.conn, ac.ActiveOrganizationID, ac.UserID, id.String())
	policy, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: selector}})
	require.NoError(t, err)
	delegated, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	token, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{OrganizationID: ac.ActiveOrganizationID, CreatedByUserID: ac.UserID, Name: "Built-in test key " + agent.ID.String(), KeyPrefix: prefix, KeyHash: hash, SubjectUrn: conv.ToPGText(urn.NewAgentSubject(agent.ID).String()), DelegatedGrants: delegated, DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
	require.NoError(t, err)
	return agent.ID, token
}

func serveBuiltinHTTP(t *testing.T, ti *testInstance, projectID uuid.UUID, token string, body []byte, headers map[string]string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	return serveBuiltinHTTPOn(t, t.Context(), ti, projectID, token, body, headers)
}

func serveBuiltinHTTPOn(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, token string, body []byte, headers map[string]string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, projectID)
	if err != nil {
		return httptest.NewRecorder(), err
	}
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, project.OrganizationID)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/mcp/builtin/"+org.Slug+"/"+project.Slug+"/slack", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	route := chi.NewRouteContext()
	route.URLParams.Add("organizationSlug", org.Slug)
	route.URLParams.Add("projectSlug", project.Slug)
	route.URLParams.Add("builtinKey", "slack")
	request = request.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	response := httptest.NewRecorder()
	if err := ti.service.ServeBuiltin(response, request); err != nil {
		return response, fmt.Errorf("serve built-in test request: %w", err)
	}
	return response, nil
}

func TestServeBuiltin_ExplicitMachineOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	_, token := seedBuiltinAgent(t, ctx, ti, true)
	_, broad := seedBuiltinAgent(t, ctx, ti, false)
	body := makeMetaRPCBody(t, "tools/list", map[string]any{})
	_, err := serveBuiltinHTTP(t, ti, *ac.ProjectID, "", body, nil)
	requireAgentGatewayCode(t, err, oops.CodeUnauthorized)
	_, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, broad, body, nil)
	require.Error(t, err)
	response, err := serveBuiltinHTTP(t, ti, *ac.ProjectID, token, body, nil)
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "platform_slack_send_message")
	require.NotContains(t, response.Body.String(), "platform_list_triggers")
	require.NotContains(t, response.Body.String(), "platform_slack_get_file_url")
	_, err = serveBuiltinHTTP(t, ti, uuid.New(), token, body, nil)
	require.Error(t, err)
	// No environment means a clear credential error, not an OAuth challenge.
	response, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, token, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "platform_slack_read_channel_messages", "arguments": map[string]any{}}), nil)
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "configure a Slack credential")
	require.Empty(t, response.Header().Get("WWW-Authenticate"))
}

func TestServeBuiltin_HiddenRemainsAddressableAndDerived(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	agent, token := seedBuiltinAgent(t, ctx, ti, true)
	environment, err := envrepo.New(ti.conn).CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Slack fixture", Slug: "slack-fixture"})
	require.NoError(t, err)
	_, err = envrepo.New(ti.conn).UpsertEnvironmentEntry(ctx, envrepo.UpsertEnvironmentEntryParams{Name: "SLACK_BOT_TOKEN", Value: "fixture-token", EnvironmentID: environment.ID, ProjectID: *ac.ProjectID})
	require.NoError(t, err)
	_, err = builtinrepo.New(ti.conn).InsertSettings(ctx, builtinrepo.InsertSettingsParams{ProjectID: conv.ToNullUUID(*ac.ProjectID), BuiltinKey: "slack", Hidden: true, EnvironmentID: conv.ToNullUUID(environment.ID)})
	require.NoError(t, err)
	response, err := serveBuiltinHTTP(t, ti, *ac.ProjectID, token, makeMetaRPCBody(t, "tools/list", map[string]any{}), nil)
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "platform_slack_send_message")
	response, err = serveAgentGatewayHTTP(t, ti, agent.String(), token, listServersBody(t))
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "builtin_slack")
	// A wrong explicit override never falls back to the configured account.
	response, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, token, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "platform_slack_read_channel_messages", "arguments": map[string]any{}}), map[string]string{"Gram-Environment": "missing"})
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "built-in environment is unavailable")
}

func TestServeBuiltin_InheritsStandardDefaultEnvironment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	agent, token := seedBuiltinAgent(t, ctx, ti, true)
	environment, err := envrepo.New(ti.conn).GetEnvironmentBySlug(ctx, envrepo.GetEnvironmentBySlugParams{Slug: "default", ProjectID: *ac.ProjectID})
	require.NoError(t, err)
	_, err = envrepo.New(ti.conn).UpsertEnvironmentEntry(ctx, envrepo.UpsertEnvironmentEntryParams{Name: "SLACK_BOT_TOKEN", Value: "fixture-default-token", EnvironmentID: environment.ID, ProjectID: *ac.ProjectID})
	require.NoError(t, err)
	// No builtin settings row or member override: derive from project default.
	response, err := serveAgentGatewayHTTP(t, ti, agent.String(), token, listServersBody(t))
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "builtin_slack")
	_, err = envrepo.New(ti.conn).DeleteEnvironment(ctx, envrepo.DeleteEnvironmentParams{Slug: "default", ProjectID: *ac.ProjectID})
	require.NoError(t, err)
	response, err = serveAgentGatewayHTTP(t, ti, agent.String(), token, listServersBody(t))
	require.NoError(t, err)
	require.NotContains(t, response.Body.String(), "builtin_slack")
	// Reject before dispatch without stored or explicit header credentials.
	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "not_a_builtin_tool", "arguments": map[string]any{}})
	response, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, token, body, nil)
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "built-in environment is unavailable")
	// Header-only credentials remain usable for legacy projects without default.
	// Unknown tool keeps this assertion entirely local, after env resolution.
	response, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, token, body, map[string]string{"MCP-SLACK-BOT-TOKEN": "fixture-header-token"})
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "tool not found")
	require.NotContains(t, response.Body.String(), "built-in environment is unavailable")
	// An explicit missing override never falls back to headers/default.
	response, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, token, body, map[string]string{"MCP-SLACK-BOT-TOKEN": "fixture-header-token", "Gram-Environment": "missing"})
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "built-in environment is unavailable")
}

func TestServeBuiltin_CustomDomainLockdown(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	_, token := seedBuiltinAgent(t, ctx, ti, true)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{OrganizationID: ac.ActiveOrganizationID, Domain: "builtin-lockdown-" + uuid.NewString()[:8] + ".example.com", IpAllowlist: []string{"192.0.2.0/24"}})
	require.NoError(t, err)
	_, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, token, makeInitializeBody(), nil)
	requireAgentGatewayCode(t, err, oops.CodeForbidden)
	own := customdomains.WithContext(t.Context(), &customdomains.Context{OrganizationID: ac.ActiveOrganizationID, Domain: domain.Domain, DomainID: domain.ID})
	response, err := serveBuiltinHTTPOn(t, own, ti, *ac.ProjectID, token, makeInitializeBody(), nil)
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "Speakeasy Built-in MCP")
	foreign := customdomains.WithContext(t.Context(), &customdomains.Context{OrganizationID: "org_other", Domain: "other.example.com", DomainID: uuid.New()})
	_, err = serveBuiltinHTTPOn(t, foreign, ti, *ac.ProjectID, token, makeInitializeBody(), nil)
	requireAgentGatewayCode(t, err, oops.CodeNotFound)
}

func TestServeBuiltin_RejectsInvalidEnvelope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	_, token := seedBuiltinAgent(t, ctx, ti, true)
	for _, tc := range []struct {
		name string
		body []byte
		code oops.Code
	}{
		{"malformed", []byte(`{"jsonrpc":`), oops.CodeParseError},
		{"batch", []byte(`[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`), oops.CodeBadRequest},
		{"wrong version", []byte(`{"jsonrpc":"1.0","id":1,"method":"tools/list"}`), oops.CodeBadRequest},
		{"oversized", bytes.Repeat([]byte("x"), (1<<20)+1), oops.CodeRequestTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := serveBuiltinHTTP(t, ti, *ac.ProjectID, token, tc.body, nil)
			requireAgentGatewayCode(t, err, tc.code)
		})
	}
}
