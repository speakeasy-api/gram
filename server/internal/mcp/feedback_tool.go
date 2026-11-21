package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// SetFeedback has the gateway serve the feedback tool on the MCP servers that
// offer it. Without a recorder no server lists the tool.
//
// The tool is served on requests addressed to a server directly. A gateway
// dispatching to that server as one of its members does not list it, so a
// gateway in front of several servers does not offer several feedback tools.
func (s *Service) SetFeedback(recorder *mcpfeedback.Recorder) {
	s.feedback = recorder
}

// hostedFeedbackTool returns the feedback tool the request's server offers.
// A failed lookup serves the request without the tool: the server's own tools
// must stay reachable when feedback cannot be loaded.
func hostedFeedbackTool(ctx context.Context, logger *slog.Logger, feedback *mcpfeedback.Recorder, payload *mcpInputs) (mcpfeedback.Tool, bool) {
	none := mcpfeedback.Tool{Name: "", Title: "", Description: "", InputSchema: nil}
	if feedback == nil {
		return none, false
	}
	serverID := payload.servingServerID()
	if serverID == nil {
		return none, false
	}

	tool, ok, err := feedback.Tool(ctx, payload.projectID, *serverID)
	if err != nil {
		logger.ErrorContext(ctx, "load mcp feedback tool; serving without it", attr.SlogError(err), attr.SlogMcpServerID(serverID.String()))
		return none, false
	}

	return tool, ok
}

// listHostedFeedbackTool adds the server's feedback tool to a hosted
// tools/list. The tool's name is taken on the server: a tool of the server's
// own with the same name is left out, exactly as a call to that name is
// answered by the feedback tool. It is added after every filter has run,
// because access to it follows access to the server rather than to any one
// of the server's tools.
func listHostedFeedbackTool(ctx context.Context, logger *slog.Logger, feedback *mcpfeedback.Recorder, payload *mcpInputs, tools []*toolListEntry) []*toolListEntry {
	tool, ok := hostedFeedbackTool(ctx, logger, feedback, payload)
	if !ok {
		return tools
	}

	listed := make([]*toolListEntry, 0, len(tools)+1)
	for _, entry := range tools {
		if entry.Name != tool.Name {
			listed = append(listed, entry)
		}
	}

	return append(listed, &toolListEntry{
		Title:        tool.Title,
		OutputSchema: nil,
		Icons:        nil,
		Execution:    nil,
		Name:         tool.Name,
		Description:  tool.Description,
		InputSchema:  tool.InputSchema,
		Annotations: &externalmcp.ToolAnnotations{
			Title:           tool.Title,
			ReadOnlyHint:    new(false),
			DestructiveHint: new(false),
			IdempotentHint:  new(false),
			OpenWorldHint:   new(true),
		},
		Meta: nil,
	})
}

// handleHostedFeedbackToolCall answers a hosted tools/call that names the
// server's feedback tool. The call is not one of the server's tool calls: it
// is neither billed nor written to the tool logs.
func handleHostedFeedbackToolCall(
	ctx context.Context,
	logger *slog.Logger,
	feedback *mcpfeedback.Recorder,
	clientInfoStore sessionClientInfoStore,
	payload *mcpInputs,
	reqID mcpjsonrpc.ID,
	organizationID string,
	serverID uuid.UUID,
	arguments json.RawMessage,
	clientHint *mcprequests.SanitizedClientInfo,
) (json.RawMessage, error) {
	client, _ := resolveClientIdentity(ctx, logger, clientInfoStore, payload, clientHint)

	caller := feedbackCaller(ctx, organizationID, payload.request, payload.authenticated)
	caller.ClientName = client.Name
	caller.ClientVersion = client.Version

	outcome := feedback.Call(ctx, mcpfeedback.Target{
		OrganizationID: organizationID,
		ProjectID:      payload.projectID,
		McpServerID:    serverID,
	}, caller, arguments)

	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		Text:     outcome.Text,
		MimeType: nil,
		Data:     nil,
		Meta:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize feedback result chunk").LogError(ctx, logger)
	}

	response, err := json.Marshal(result[toolCallResult]{
		ID: reqID,
		Result: toolCallResult{
			Content:           []json.RawMessage{chunk},
			StructuredContent: outcome.Structured,
			IsError:           outcome.IsError,
		},
		serverIdentity: serverInfoHostedToolset,
		cacheHints:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize feedback response").LogError(ctx, logger)
	}

	return response, nil
}

// feedbackCaller names who sent a report, as far as the request says. Nobody
// is named unless the request was authenticated, and a person is named only
// when they belong to the server's organization, whose people that
// organization's feedback may name.
//
// The caller's rate limit follows the most durable identity the request has,
// whatever its organization: its principal, else its API key, else the MCP
// session the client sent. The session only picks the bucket and is never
// stored, because on a public server it is the caller's only credential.
func feedbackCaller(ctx context.Context, organizationID string, r *http.Request, authenticated bool) mcpfeedback.Caller {
	caller := mcpfeedback.Caller{UserID: "", UserEmail: "", ExternalUserID: "", ClientName: "", ClientVersion: "", LimitSubject: ""}
	if r != nil {
		if session := r.Header.Get(proxy.McpSessionIDHeader); session != "" {
			caller.LimitSubject = "session:" + session
		}
	}
	if !authenticated {
		return caller
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return caller
	}
	switch {
	case authCtx.UserID != "":
		caller.LimitSubject = "user:" + authCtx.UserID
	case authCtx.ExternalUserID != "":
		caller.LimitSubject = "external:" + authCtx.ExternalUserID
	case authCtx.APIKeyID != "":
		caller.LimitSubject = "api-key:" + authCtx.APIKeyID
	}
	if authCtx.ActiveOrganizationID == organizationID {
		caller.ExternalUserID = authCtx.ExternalUserID
		caller.UserID = authCtx.UserID
		if authCtx.Email != nil {
			caller.UserEmail = *authCtx.Email
		}
	}

	return caller
}

// feedbackLocalTools has a proxied server's feedback tool answered here
// instead of being forwarded. The proxy resolves it only for requests that
// list or call tools, so every other request to the server costs nothing.
func (s *Service) feedbackLocalTools(logger *slog.Logger, organizationID string, projectID uuid.UUID, mcpServer *mcpserversrepo.McpServer) remotemcp.BuildOption {
	feedback := s.feedback
	if feedback == nil {
		return remotemcp.WithLocalTools(nil)
	}

	target := mcpfeedback.Target{OrganizationID: organizationID, ProjectID: projectID, McpServerID: mcpServer.ID}
	clientScope := mcpServerClientInfoScope(mcpServer)

	return remotemcp.WithLocalTools(func(ctx context.Context) ([]proxy.LocalTool, error) {
		tool, ok, err := feedback.Tool(ctx, projectID, mcpServer.ID)
		if err != nil {
			return nil, fmt.Errorf("load mcp feedback tool: %w", err)
		}
		if !ok {
			return nil, nil
		}

		return []proxy.LocalTool{{
			Definition: &sdkmcp.Tool{
				Meta: nil,
				Annotations: &sdkmcp.ToolAnnotations{
					DestructiveHint: new(false),
					IdempotentHint:  false,
					OpenWorldHint:   new(true),
					ReadOnlyHint:    false,
					Title:           tool.Title,
				},
				Description:  tool.Description,
				InputSchema:  tool.InputSchema,
				Name:         tool.Name,
				OutputSchema: nil,
				Title:        tool.Title,
				Icons:        nil,
			},
			Call: func(ctx context.Context, call *proxy.ToolsCallRequest) (*sdkmcp.CallToolResult, error) {
				// A proxied request carries an auth context only when its
				// caller presented a credential.
				caller := feedbackCaller(ctx, organizationID, call.UserRequest.UserHTTPRequest, true)
				sessionID := ""
				if call.UserRequest.UserHTTPRequest != nil {
					sessionID = call.UserRequest.UserHTTPRequest.Header.Get(proxy.McpSessionIDHeader)
				}
				caller.ClientName, caller.ClientVersion = s.proxiedFeedbackClient(ctx, logger, projectID, clientScope, sessionID)

				outcome := feedback.Call(ctx, target, caller, call.Params.Arguments)

				called := &sdkmcp.CallToolResult{
					Meta:              nil,
					Content:           []sdkmcp.Content{&sdkmcp.TextContent{Meta: nil, Annotations: nil, Text: outcome.Text}},
					StructuredContent: nil,
					IsError:           outcome.IsError,
					InputRequests:     nil,
					RequestState:      "",
				}
				if len(outcome.Structured) > 0 {
					called.StructuredContent = outcome.Structured
				}
				return called, nil
			},
		}}, nil
	})
}

// proxiedFeedbackClient names the MCP client behind a proxied call: the one
// the gateway already resolved for the request, else the one the call's
// session reported at its handshake.
func (s *Service) proxiedFeedbackClient(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, scope, sessionID string) (name, version string) {
	if info, ok := contextvalues.GetMCPClientInfo(ctx); ok {
		return info.Name, info.Version
	}
	if s.tunnelClientFamilies == nil || scope == "" || sessionID == "" {
		return "", ""
	}

	return s.tunnelClientFamilies.forRow(logger, projectID, scope, sessionID)
}
