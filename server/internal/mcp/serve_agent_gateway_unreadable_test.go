package mcp_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// An agent's attached connection that can't be read is never used without credentials:
// the agent is sent to someone who can edit it, as for any connection its sign-in step fixes.
func TestServeAgentGateway_UnreadableConnectionAsksEditorsToReconnect(t *testing.T) {
	t.Parallel()
	ctx, seeded := seedOwnerAttachedMember(t)
	ti, fx := seeded.ti, seeded.fx
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := remotesessionsrepo.New(ti.conn)
	current, err := q.GetActiveRemoteSession(ctx, remotesessionsrepo.GetActiveRemoteSessionParams{SubjectUrn: urn.NewUserSubject(authCtx.UserID), RemoteSessionClientID: seeded.binding.RemoteSessionClientID})
	require.NoError(t, err)
	_, err = q.UpdateRemoteSessionTokensIfUnchanged(ctx, remotesessionsrepo.UpdateRemoteSessionTokensIfUnchangedParams{
		SubjectUrn: current.SubjectUrn, RemoteSessionClientID: current.RemoteSessionClientID, ExpectedUpdatedAt: current.UpdatedAt,
		AccessTokenEncrypted: "not-ciphertext", AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: current.Scopes,
	})
	require.NoError(t, err)

	agent, err := agentsrepo.New(ti.conn).GetAgentByID(ctx, agentsrepo.GetAgentByIDParams{OrganizationID: fx.agent.OrganizationID, ID: fx.agent.ID})
	require.NoError(t, err)
	projectSlug := conv.PtrValOr(authCtx.ProjectSlug, "")
	if agent.ProjectID.Valid {
		project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, agent.ProjectID.UUID)
		require.NoError(t, err)
		projectSlug = project.Slug
	}
	link := fmt.Sprintf("/%s/projects/%s/identities/agent:%s/mcp-sign-in", authCtx.OrganizationSlug, projectSlug, fx.agent.ID)

	call := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute_tool", "arguments": map[string]any{"name": fx.granted + "--ping", "arguments": map[string]any{}}})
	response, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, call)
	require.NoError(t, err)
	text, isError := metaToolResultText(t, decodeRPCResponse(t, response))
	require.True(t, isError, text)
	require.Contains(t, text, fmt.Sprintf("server %q needs re-authentication", fx.granted))
	require.Contains(t, text, "Ask someone who can edit this agent to open ")
	require.Contains(t, text, link+" and reconnect it, then retry")
	require.NotContains(t, text, "administrator")

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStoredSignInUnreadable)
	require.Equal(t, current.ID.String(), row.RemoteSessionID)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)

	status := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "connection_status", "arguments": map[string]any{}})
	response, err = serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, status)
	require.NoError(t, err)
	rpc := decodeRPCResponse(t, response)
	var result struct {
		StructuredContent metamcp.ConnectionStatusResult `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(rpc["result"], &result))
	server := slices.IndexFunc(result.StructuredContent.Servers, func(s metamcp.ConnectionStatusServer) bool { return s.Slug == fx.granted })
	require.GreaterOrEqual(t, server, 0)
	require.True(t, result.StructuredContent.Servers[server].Reconnect, "the agent's sign-in step fixes it")
	require.Equal(t, "needs re-authentication", result.StructuredContent.Servers[server].Detail)
	require.Nil(t, result.StructuredContent.Servers[server].Outage)

	seeded.upstream.mu.Lock()
	defer seeded.upstream.mu.Unlock()
	for _, request := range seeded.upstream.requests {
		require.NotEqual(t, "tools/call", request.rpcMethod, "the member is never called without credentials")
	}
}
