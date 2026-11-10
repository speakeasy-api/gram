package admin

import (
	"context"
	"errors"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

// SetMCPDebugPolicy enables the MCP debugger with the network policy its
// outbound requests run under.
func (s *Service) SetMCPDebugPolicy(policy *guardian.Policy) {
	s.mcpDebugPolicy = policy
}

// DebugMcpURL traces a remote MCP server URL for staff, as an anonymous
// client: it sends no credentials and stores nothing. Hosts the network policy
// refuses come back as an unreachable trace rather than an error, so staff see
// which step stopped.
func (s *Service) DebugMcpURL(ctx context.Context, p *gen.DebugMcpURLPayload) (*gen.AdminMcpDebugReport, error) {
	if s.mcpDebugPolicy == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}

	debugCtx, cancel := context.WithTimeout(ctx, remotemcp.DebugURLTimeout)
	defer cancel()

	if _, err := proxy.ValidateRemoteMCPURL(debugCtx, s.mcpDebugPolicy, p.URL); err != nil &&
		!errors.Is(err, guardian.ErrBlockedIP) && !errors.Is(err, guardian.ErrBadHost) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid url")
	}

	actor, _, _ := adminActor(ctx)
	s.logger.InfoContext(ctx, "staff debugged an mcp url",
		attr.SlogAuthorizationActorID(actor.String()),
		attr.SlogURLFull(urls.DiagnosticURL(p.URL)),
	)

	report := remotemcp.DebugRemoteMcpURL(debugCtx, s.mcpDebugPolicy, p.URL, remotemcp.DebugOptions{ClientMetadataOrigin: s.mcpServerURL})
	return buildAdminMcpDebugReport(report), nil
}

func buildAdminMcpDebugReport(r *remotemcp.DebugReport) *gen.AdminMcpDebugReport {
	steps := make([]*gen.AdminMcpDebugStep, 0, len(r.Steps))
	for _, step := range r.Steps {
		steps = append(steps, &gen.AdminMcpDebugStep{
			ID:         gen.AdminMcpDebugStepID(step.ID),
			Title:      step.Title,
			Status:     gen.AdminMcpDebugStepStatus(step.Status),
			Summary:    step.Summary,
			DurationMs: step.DurationMs,
			RequestURL: step.RequestURL,
			HTTPStatus: step.HTTPStatus,
			Details:    step.Details,
		})
	}

	var tools []*gen.AdminMcpDebugTool
	if r.Tools != nil {
		tools = make([]*gen.AdminMcpDebugTool, 0, len(r.Tools))
		for _, tool := range r.Tools {
			tools = append(tools, &gen.AdminMcpDebugTool{
				Name:        tool.Name,
				Title:       tool.Title,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
				Annotations: tool.Annotations,
			})
		}
	}

	var prompts []*gen.AdminMcpDebugPrompt
	if r.Prompts != nil {
		prompts = make([]*gen.AdminMcpDebugPrompt, 0, len(r.Prompts))
		for _, prompt := range r.Prompts {
			prompts = append(prompts, &gen.AdminMcpDebugPrompt{
				Name:        prompt.Name,
				Title:       prompt.Title,
				Description: prompt.Description,
				Arguments:   prompt.Arguments,
			})
		}
	}

	var resources []*gen.AdminMcpDebugResource
	if r.Resources != nil {
		resources = make([]*gen.AdminMcpDebugResource, 0, len(r.Resources))
		for _, resource := range r.Resources {
			resources = append(resources, &gen.AdminMcpDebugResource{
				URI:         resource.URI,
				Name:        resource.Name,
				Title:       resource.Title,
				Description: resource.Description,
				MimeType:    resource.MimeType,
			})
		}
	}

	return &gen.AdminMcpDebugReport{
		URL:             r.URL,
		Outcome:         gen.AdminMcpDebugOutcome(r.Outcome),
		DurationMs:      r.DurationMs,
		Steps:           steps,
		AuthMethod:      gen.AdminMcpDebugAuthMethod(r.AuthMethod),
		ProtocolVersion: r.ProtocolVersion,
		ServerName:      r.ServerName,
		ServerVersion:   r.ServerVersion,
		Instructions:    r.Instructions,
		Tools:           tools,
		Prompts:         prompts,
		Resources:       resources,
	}
}
