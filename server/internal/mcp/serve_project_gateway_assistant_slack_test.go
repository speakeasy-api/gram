package mcp_test

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServeProjectGateway_BuiltinAssistantSlackToolset(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	ctx := f.ctx
	ti := f.ti
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, ctx, ti)
	grantProjectRead(t, ti, authCtx)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	// Record outbound Slack API calls made by the assistant-slack built-in.
	var mu sync.Mutex
	var capturedAuthHeaders []string
	ti.slackTransport.set(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		capturedAuthHeaders = append(capturedAuthHeaders, req.Header.Get("Authorization"))
		mu.Unlock()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"channel":"C123","ts":"1700000000.000100"}`)),
			Request:    req,
		}, nil
	})

	// Seed two distinct slack_app auth clients in the same project (no persisted
	// Slack toolsets and no project environments).
	seedSlackAppClient := func(name, botToken string) uuid.UUID {
		clientID, err := testrepo.New(ti.conn).CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			ClientType:     authz.AuthClientTypeSlackApp,
			Name:           name,
		})
		require.NoError(t, err)
		encToken, err := ti.enc.Encrypt([]byte(botToken))
		require.NoError(t, err)
		require.NoError(t, testrepo.New(ti.conn).CreateAuthClientHeaderFixture(ctx, testrepo.CreateAuthClientHeaderFixtureParams{
			AuthClientID:   clientID,
			OrganizationID: authCtx.ActiveOrganizationID,
			Name:           authclients.SlackHeaderBotToken,
			IsSecret:       true,
			Value:          pgtype.Text{String: encToken, Valid: true},
		}))
		return clientID
	}
	clientA := seedSlackAppClient("Assistant A Slack", "xoxb-assistant-a-secret")
	clientB := seedSlackAppClient("Assistant B Slack", "xoxb-assistant-b-secret")

	// Owner holds mcp:connect on assistant-slack and wildcard mcp:auth (as default roles do).
	assistantSlackID := platformtools.PlatformToolsetID(platformtools.AssistantSlackPlatformToolsetSlug)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, assistantSlackID.String())
	ownerAuthSel, err := authz.AuthClientSelector(authz.WildcardResource, "").MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		Scope:          string(authz.ScopeMCPAuth),
		Selectors:      ownerAuthSel,
	})
	require.NoError(t, err)

	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
	bindingA := seedAssistantThreadBinding(t, ctx, ti, authCtx)

	humanTurnTokenA, err := ti.assistantTokens.MintTurnCredential(ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 agentPrincipal,
		AuthorizerUserID:          authCtx.UserID,
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, bindingA)
	require.NoError(t, err)

	workloadTurnTokenA, err := ti.assistantTokens.MintTurnCredential(ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewWorkloadPrincipal(f.issuerID, f.subject),
		AuthorizerUserID:          "",
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, bindingA)
	require.NoError(t, err)

	// 1. Without mcp:connect on assistant-slack, the built-in is hidden.
	listed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, humanTurnTokenA, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code)
	require.NotContains(t, listed.Body.String(), `"slug":"assistant-slack"`)

	// 2. Grant mcp:connect on assistant-slack to the agent, but ZERO mcp:auth grants ->
	// listed in gateway, but calling send_message fails closed with a clear tool error.
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, agentPrincipal, assistantSlackID)

	listed, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, humanTurnTokenA, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"slug":"assistant-slack"`)

	qualifiedSend := platformtools.AssistantSlackPlatformToolsetSlug + "--" + "platform_slack_send_message"
	callSend := func(token string) (string, bool) {
		resp, callErr := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
			"name": metamcp.ToolExecuteTool,
			"arguments": map[string]any{
				"name":      qualifiedSend,
				"arguments": map[string]any{"channel_id": "C123", "text": "hello from project gateway"},
			},
		}))
		require.NoError(t, callErr)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		return metaToolResultText(t, decodeRPCResponse(t, resp))
	}

	zeroText, zeroErr := callSend(humanTurnTokenA)
	require.True(t, zeroErr, "zero authorized slack_app clients must fail closed")
	require.Contains(t, zeroText, "no Slack app auth client is authorized")

	// 3. Grant the agent mcp:auth on clientA ONLY -> BOTH human-initiated AND workload turns reply via clientA's bot token.
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   agentPrincipal,
		Scope:          string(authz.ScopeMCPAuth),
		Selectors:      mustMarshalSelector(t, assistantidentity.AssistantSlackAuthGrant(clientA).Selector),
	})
	require.NoError(t, err)

	humanText, humanErr := callSend(humanTurnTokenA)
	require.False(t, humanErr, humanText)
	require.Contains(t, humanText, `"ok":true`)

	workloadText, workloadErr := callSend(workloadTurnTokenA)
	require.False(t, workloadErr, workloadText)
	require.Contains(t, workloadText, `"ok":true`)

	mu.Lock()
	require.Equal(t, []string{"Bearer xoxb-assistant-a-secret", "Bearer xoxb-assistant-a-secret"}, capturedAuthHeaders)
	mu.Unlock()

	// 4. Also granting the agent mcp:auth on clientB -> ambiguous (>1 matching clients) fails closed.
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   agentPrincipal,
		Scope:          string(authz.ScopeMCPAuth),
		Selectors:      mustMarshalSelector(t, assistantidentity.AssistantSlackAuthGrant(clientB).Selector),
	})
	require.NoError(t, err)

	ambigText, ambigErr := callSend(humanTurnTokenA)
	require.True(t, ambigErr, "multiple authorized slack_app clients must fail closed")
	require.Contains(t, ambigText, "multiple Slack app auth clients")

	// 5. speakeasy-slack stays managed-only even if the ordinary assistant holds mcp:connect on it.
	speakeasySlackID := platformtools.PlatformToolsetID(platformtools.SpeakeasySlackPlatformToolsetSlug)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, speakeasySlackID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, agentPrincipal, speakeasySlackID)
	afterSpeakeasyGrant, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, humanTurnTokenA, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, afterSpeakeasyGrant.Code, afterSpeakeasyGrant.Body.String())
	require.NotContains(t, afterSpeakeasyGrant.Body.String(), `"slug":"speakeasy-slack"`)
}

func mustMarshalSelector(t *testing.T, sel authz.Selector) []byte {
	t.Helper()
	b, err := sel.MarshalJSON()
	require.NoError(t, err)
	return b
}
