package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// SetFeedback has the gateway serve the feedback tool on the MCP servers that
// offer it, as a local tool. Without a recorder no server lists the tool.
//
// Like any local tool, it is offered wherever the server's own tools are:
// directly, and through a gateway the server is a member of, where its name
// is qualified with the member's slug. A report sent through a gateway is
// filed against the member it names.
func (s *Service) SetFeedback(recorder *mcpfeedback.Recorder) {
	if recorder == nil {
		return
	}
	s.localToolSources = append(s.localToolSources, feedbackLocalTools(recorder))
}

// feedbackLocalTools offers a server's feedback tool, when it has one on.
func feedbackLocalTools(recorder *mcpfeedback.Recorder) localToolSource {
	return func(ctx context.Context, server localToolServer) ([]localTool, error) {
		tool, ok, err := recorder.Tool(ctx, server.projectID, server.mcpServerID)
		if err != nil {
			return nil, fmt.Errorf("load mcp feedback tool: %w", err)
		}
		if !ok {
			return nil, nil
		}

		target := mcpfeedback.Target{OrganizationID: server.organizationID, ProjectID: server.projectID, McpServerID: server.mcpServerID}
		return []localTool{{
			definition: &sdkmcp.Tool{
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
			call: func(ctx context.Context, call localToolCall) (*sdkmcp.CallToolResult, error) {
				caller := feedbackCaller(ctx, server.organizationID, call.request, call.authenticated)
				caller.ClientName = call.clientName
				caller.ClientVersion = call.clientVersion

				outcome := recorder.Call(ctx, target, caller, call.arguments)
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
	}
}

// feedbackCaller names who sent a report, as far as the request says. Nobody
// is named unless the request was authenticated, and a person is named only
// when they belong to the server's organization, whose people that
// organization's feedback may name. The session is the one the client sent,
// never one the gateway made up for a request without one.
func feedbackCaller(ctx context.Context, organizationID string, r *http.Request, authenticated bool) mcpfeedback.Caller {
	caller := mcpfeedback.Caller{SessionID: "", UserID: "", UserEmail: "", ExternalUserID: "", ClientName: "", ClientVersion: "", LimitSubject: ""}
	if r != nil {
		caller.SessionID = r.Header.Get(proxy.McpSessionIDHeader)
	}
	if !authenticated {
		return caller
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return caller
	}
	// A caller from another organization is not named, but is still held to
	// its own rate limit rather than only the server's.
	caller.LimitSubject = authCtx.UserID
	if caller.LimitSubject == "" {
		caller.LimitSubject = authCtx.ExternalUserID
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

// proxiedClient names the MCP client behind a proxied call: the one
// the gateway already resolved for the request, else the one the call's
// session reported at its handshake.
func (s *Service) proxiedClient(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, scope, sessionID string) (name, version string) {
	if info, ok := contextvalues.GetMCPClientInfo(ctx); ok {
		return info.Name, info.Version
	}
	if s.tunnelClientFamilies == nil || scope == "" || sessionID == "" {
		return "", ""
	}

	return s.tunnelClientFamilies.forRow(logger, projectID, scope, sessionID)
}
