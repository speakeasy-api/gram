//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type MCPReviewRequestService interface {
	CreatePlatformRequest(ctx context.Context, organizationID string, projectID uuid.UUID, userID, targetKind, target, note string) (*gen.ApprovalRequestSummary, error)
	ReadPlatformRequesterReview(ctx context.Context, input mcpapproval.PlatformRequesterReviewInput) (mcpapproval.PlatformRequesterReview, error)
}

type MCPReviewProjectResolver interface {
	ResolveReviewProject(ctx context.Context, principal Principal, projectID string) (ResolvedProject, error)
}

type RequestMCPReviewInput struct {
	ProjectID     string `json:"project_id" jsonschema:"explicit project ID returned by list_projects"`
	TargetKind    string `json:"target_kind" jsonschema:"server_url or stdio_command"`
	Target        string `json:"target" jsonschema:"server URL or command to review; credential-shaped values are redacted before storage"`
	Justification string `json:"justification" jsonschema:"why you need this MCP server; required and visible to reviewers"`
}

type RequestMCPReviewOutput struct {
	RequestID  string `json:"request_id"`
	ProjectID  string `json:"project_id"`
	TargetKind string `json:"target_kind"`
	Target     string `json:"target"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	NextAction string `json:"next_action"`
}

type GetMyMCPReviewRequestInput struct {
	ProjectID string `json:"project_id" jsonschema:"explicit project ID returned by list_projects"`
	RequestID string `json:"request_id" jsonschema:"request ID returned by request_mcp_review"`
}

type GetMyMCPReviewRequestOutput struct {
	ProjectID string `json:"project_id"`
	mcpapproval.PlatformRequesterReview
}

func registerReviewRequestTools(reg *Registrar, service MCPReviewRequestService, projects MCPReviewProjectResolver) {
	addTool(reg, &mcp.Tool{
		Name:        "request_mcp_review",
		Title:       "Request Review of an MCP Server",
		Description: "Ask an organization administrator to review an MCP server for one project. This records a request only: it does not grant access, change roles, or approve the server. Credential-shaped values in URLs and commands are redacted before storage.",
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input RequestMCPReviewInput) (*mcp.CallToolResult, RequestMCPReviewOutput, error) {
		return principalToolCall(ctx, reviewRequestToolResult, func(principal Principal) (RequestMCPReviewOutput, error) {
			projectID, err := reviewRequestProject(ctx, service, projects, principal, input.ProjectID)
			if err != nil {
				return RequestMCPReviewOutput{}, err
			}
			created, err := service.CreatePlatformRequest(ctx, principal.OrganizationID, projectID, principal.UserID, input.TargetKind, input.Target, input.Justification)
			if err != nil {
				return RequestMCPReviewOutput{}, fmt.Errorf("create MCP review request: %w", err)
			}
			return RequestMCPReviewOutput{
				RequestID: created.ID, ProjectID: projectID.String(), TargetKind: created.TargetKind, Target: created.TargetRaw,
				Status: created.Status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt, NextAction: reviewRequestNextAction(created.Status),
			}, nil
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_my_mcp_review_request",
		Title:       "Get My MCP Review Request",
		Description: "Check the status of one MCP review request you submitted. It returns only your own privacy-minimized request state, never other requesters, notes, evidence, research, or administrator rationale.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMyMCPReviewRequestInput) (*mcp.CallToolResult, GetMyMCPReviewRequestOutput, error) {
		return principalToolCall(ctx, reviewRequestToolResult, func(principal Principal) (GetMyMCPReviewRequestOutput, error) {
			projectID, err := reviewRequestProject(ctx, service, projects, principal, input.ProjectID)
			if err != nil {
				return GetMyMCPReviewRequestOutput{}, err
			}
			requestID, err := uuid.Parse(input.RequestID)
			if err != nil {
				return GetMyMCPReviewRequestOutput{}, oops.E(oops.CodeBadRequest, err, "request_id must be a UUID")
			}
			request, err := service.ReadPlatformRequesterReview(ctx, mcpapproval.PlatformRequesterReviewInput{
				OrganizationID: principal.OrganizationID, ProjectID: projectID, UserID: principal.UserID, RequestID: requestID,
			})
			if err != nil {
				return GetMyMCPReviewRequestOutput{}, fmt.Errorf("read own MCP review request: %w", err)
			}
			return GetMyMCPReviewRequestOutput{ProjectID: projectID.String(), PlatformRequesterReview: request}, nil
		})
	})
}

func reviewRequestProject(ctx context.Context, service MCPReviewRequestService, projects MCPReviewProjectResolver, principal Principal, rawProjectID string) (uuid.UUID, error) {
	if service == nil || projects == nil {
		return uuid.Nil, ErrUnavailable
	}
	project, err := projects.ResolveReviewProject(ctx, principal, rawProjectID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve MCP review project: %w", err)
	}
	return project.ID, nil
}

type reviewRequestRefusal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func reviewRequestToolResult(err error) (*mcp.CallToolResult, bool) {
	var shareable *oops.ShareableError
	if !errors.As(err, &shareable) {
		return nil, false
	}
	result := reviewRequestRefusal{}
	switch shareable.Code {
	case oops.CodeBadRequest, oops.CodeInvalid:
		result.Code, result.Message = "invalid_request", shareable.Error()
	case oops.CodeNotFound:
		result.Code, result.Message = "not_found", "That project or review request is not available to you."
	case oops.CodeForbidden:
		result.Code, result.Message = "forbidden", "You do not have permission to submit or read this MCP review request."
	case oops.CodeUnavailable:
		result.Code, result.Message = unavailableCode, "MCP review requests are temporarily unavailable."
	default:
		return nil, false
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}

func reviewRequestNextAction(status string) string {
	switch status {
	case "requested", "superseded":
		return "wait_for_review"
	case "approved":
		return "ask_administrator_to_grant_access"
	case "denied":
		return "contact_administrator"
	default:
		return "contact_administrator"
	}
}

func registerUnavailableReviewRequestTools(reg *Registrar) {
	for _, tool := range []struct {
		name, title, description string
		readOnly                 bool
	}{
		{"request_mcp_review", "Request Review of an MCP Server", "Ask an administrator to review an MCP server. This is not switched on for your organization yet.", false},
		{"get_my_mcp_review_request", "Get My MCP Review Request", "Check one MCP review request you submitted. This is not switched on for your organization yet.", true},
	} {
		manifest := &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description}
		if tool.readOnly {
			manifest.Annotations = readOnlyAnnotations()
		}
		addTool(reg, manifest, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableTool("mcp_review_requests"))
	}
}
