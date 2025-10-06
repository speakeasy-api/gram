package conv

import (
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func ToToolUrn(tool types.Tool) urn.Tool {
	urn := &urn.Tool{}

	if tool.HTTPToolDefinition != nil {
		urn.UnmarshalText([]byte(tool.HTTPToolDefinition.ToolUrn))
	}
	if tool.PromptTemplate != nil {
		urn.UnmarshalText([]byte(tool.PromptTemplate.ToolUrn))
	}

	panic("unknown tool type")
}

func ToBaseTool(tool *types.Tool) types.BaseToolAttributes {
	schema := constants.DefaultEmptyToolSchema

	if tool.HTTPToolDefinition != nil {
		if len(tool.HTTPToolDefinition.Schema) > 0 {
			schema = tool.HTTPToolDefinition.Schema
		}

		return types.BaseToolAttributes{
			ID:            tool.HTTPToolDefinition.ID,
			ToolUrn:       tool.HTTPToolDefinition.ToolUrn,
			ProjectID:     tool.HTTPToolDefinition.ProjectID,
			DeploymentID:  tool.HTTPToolDefinition.DeploymentID,
			Name:          tool.HTTPToolDefinition.Name,
			CanonicalName: tool.HTTPToolDefinition.CanonicalName,
			Description:   tool.HTTPToolDefinition.Description,
			SchemaVersion: tool.HTTPToolDefinition.SchemaVersion,
			Schema:        schema,
			Confirm:       tool.HTTPToolDefinition.Confirm,
			ConfirmPrompt: tool.HTTPToolDefinition.ConfirmPrompt,
			Summarizer:    tool.HTTPToolDefinition.Summarizer,
			CreatedAt:     tool.HTTPToolDefinition.CreatedAt,
			UpdatedAt:     tool.HTTPToolDefinition.UpdatedAt,
			Canonical:     tool.HTTPToolDefinition.Canonical,
			Variation:     tool.HTTPToolDefinition.Variation,
		}
	}
	if tool.PromptTemplate != nil {
		if len(tool.PromptTemplate.Schema) > 0 {
			schema = tool.PromptTemplate.Schema
		}

		return types.BaseToolAttributes{
			ID:            tool.PromptTemplate.ID,
			ToolUrn:       tool.PromptTemplate.ToolUrn,
			ProjectID:     tool.PromptTemplate.ProjectID,
			DeploymentID:  tool.PromptTemplate.DeploymentID,
			Name:          tool.PromptTemplate.Name,
			CanonicalName: tool.PromptTemplate.CanonicalName,
			Description:   tool.PromptTemplate.Description,
			SchemaVersion: tool.PromptTemplate.SchemaVersion,
			Schema:        schema,
			Confirm:       tool.PromptTemplate.Confirm,
			ConfirmPrompt: tool.PromptTemplate.ConfirmPrompt,
			Summarizer:    tool.PromptTemplate.Summarizer,
			CreatedAt:     tool.PromptTemplate.CreatedAt,
			UpdatedAt:     tool.PromptTemplate.UpdatedAt,
			Canonical:     tool.PromptTemplate.Canonical,
			Variation:     tool.PromptTemplate.Variation,
		}
	}

	panic("unknown tool type")
}
