package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agentworkflows/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type ToolsetRequest struct {
	ToolsetSlug     string
	EnvironmentSlug string
}

type LoadAgentToolsInput struct {
	OrgID           string
	ProjectID       uuid.UUID
	ToolURNs        []urn.Tool
	Toolsets        []ToolsetRequest
	EnvironmentSlug string
}

type LoadAgentToolsOutput struct {
	ToolDefs     []openrouter.Tool
	ToolMetadata map[string]ToolMetadata
}

type LoadAgentTools struct {
	logger        *slog.Logger
	agentsService *agents.Service
}

func NewLoadAgentTools(logger *slog.Logger, agentsService *agents.Service) *LoadAgentTools {
	return &LoadAgentTools{
		logger:        logger.With(attr.SlogComponent("load-agent-tools-activity")),
		agentsService: agentsService,
	}
}

func (a *LoadAgentTools) Do(ctx context.Context, input LoadAgentToolsInput) (*LoadAgentToolsOutput, error) {
	toolDefs := make([]openrouter.Tool, 0)
	toolMetadata := make(map[string]ToolMetadata)

	if len(input.ToolURNs) > 0 {
		agentTools, err := a.agentsService.LoadToolsByURN(ctx, input.ProjectID, input.ToolURNs, input.EnvironmentSlug)
		if err != nil {
			return nil, fmt.Errorf("failed to load tools by URN: %w", err)
		}

		for _, t := range agentTools {
			if t.Definition.Function != nil {
				toolDefs = append(toolDefs, t.Definition)
				toolMetadata[t.Definition.Function.Name] = ToolMetadata{
					ToolURN:         t.ToolURN,
					EnvironmentSlug: input.EnvironmentSlug,
					IsMCPTool:       t.IsMCPTool,
					ServerLabel:     t.ServerLabel,
				}
			}
		}
	}

	for _, toolset := range input.Toolsets {
		envSlug := toolset.EnvironmentSlug
		if envSlug == "" {
			envSlug = input.EnvironmentSlug
		}

		agentTools, err := a.agentsService.LoadToolsetTools(ctx, input.ProjectID, toolset.ToolsetSlug, envSlug)
		if err != nil {
			return nil, fmt.Errorf("failed to load toolset %q: %w", toolset.ToolsetSlug, err)
		}

		for _, t := range agentTools {
			if t.Definition.Function != nil {
				toolDefs = append(toolDefs, t.Definition)
				toolMetadata[t.Definition.Function.Name] = ToolMetadata{
					ToolURN:         t.ToolURN,
					EnvironmentSlug: envSlug,
					IsMCPTool:       t.IsMCPTool,
					ServerLabel:     t.ServerLabel,
				}
			}
		}
	}

	return &LoadAgentToolsOutput{
		ToolDefs:     toolDefs,
		ToolMetadata: toolMetadata,
	}, nil
}
