package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/requestip"
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
// organization's feedback may name.
//
// The caller's rate limit follows the most durable identity the request has,
// whatever its organization: its principal, else its API key, else the client
// address the edge resolved, else a hash of the MCP session the client sent.
// The session only picks the bucket, hashed because on a public server it is
// the caller's only credential.
func feedbackCaller(ctx context.Context, organizationID string, r *http.Request, authenticated bool) mcpfeedback.Caller {
	caller := mcpfeedback.Caller{UserID: "", UserEmail: "", ExternalUserID: "", ClientName: "", ClientVersion: "", LimitSubject: ""}
	if address, ok := requestip.FromContext(ctx); ok && address.Addr.IsValid() {
		caller.LimitSubject = "address:" + address.String()
	} else if r != nil {
		if session := r.Header.Get(proxy.McpSessionIDHeader); session != "" {
			digest := sha256.Sum256([]byte(session))
			caller.LimitSubject = "session:" + hex.EncodeToString(digest[:])
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
