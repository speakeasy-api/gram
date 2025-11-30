package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type LoadToolsByURNInput struct {
	OrgID           string
	ProjectID       uuid.UUID
	ToolURNs        []urn.Tool
	EnvironmentSlug string
	Headers         map[string]string
}

type LoadToolsByURNOutput struct {
	ToolDefs     []openrouter.Tool
	ToolMetadata map[string]ToolMetadata
}

type LoadToolsByURN struct {
	logger        *slog.Logger
	agentsService *agents.Service
}

func NewLoadToolsByURN(logger *slog.Logger, agentsService *agents.Service) *LoadToolsByURN {
	return &LoadToolsByURN{
		logger:        logger.With(attr.SlogComponent("load-tools-by-urn-activity")),
		agentsService: agentsService,
	}
}

func (a *LoadToolsByURN) Do(ctx context.Context, input LoadToolsByURNInput) (*LoadToolsByURNOutput, error) {
	a.logger.InfoContext(ctx, "loading tools by URN",
		attr.SlogOrganizationID(input.OrgID),
		attr.SlogProjectID(input.ProjectID.String()),
		attr.SlogValueInt(len(input.ToolURNs)))

	agentTools, err := a.agentsService.LoadToolsByURN(ctx, input.ProjectID, input.ToolURNs, input.EnvironmentSlug, input.Headers)
	if err != nil {
		return nil, fmt.Errorf("failed to load tools by URN: %w", err)
	}

	toolDefs := make([]openrouter.Tool, 0, len(agentTools))
	toolMetadata := make(map[string]ToolMetadata)

	for _, t := range agentTools {
		if t.Definition.Function != nil {
			toolDefs = append(toolDefs, t.Definition)
			toolMetadata[t.Definition.Function.Name] = ToolMetadata{
				ToolURN:         t.ToolURN,
				EnvironmentSlug: input.EnvironmentSlug,
				Headers:         input.Headers,
				IsMCPTool:       t.IsMCPTool,
				ServerLabel:     t.ServerLabel,
			}
		}
	}

	return &LoadToolsByURNOutput{
		ToolDefs:     toolDefs,
		ToolMetadata: toolMetadata,
	}, nil
}
