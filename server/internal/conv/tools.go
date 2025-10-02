package conv

import (
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func ToToolUrn(tool types.Tool) urn.Tool {
	urn := &urn.Tool{}

	switch tool.Tool.(type) {
	case *types.HTTPToolDefinition:
		urn.UnmarshalText([]byte(tool.Tool.(*types.HTTPToolDefinition).ToolUrn))
	case *types.PromptTemplate:
		urn.UnmarshalText([]byte(tool.Tool.(*types.PromptTemplate).ToolUrn))
	}

	return *urn
}

func ToBaseTool(tool *types.Tool) *types.BaseToolAttributes {
	switch tool.Tool.(type) {
	case *types.HTTPToolDefinition:
		return &types.BaseToolAttributes{
			Type: tool.Tool.(*types.HTTPToolDefinition).Type,
			ID: tool.Tool.(*types.HTTPToolDefinition).ID,
			ToolUrn: tool.Tool.(*types.HTTPToolDefinition).ToolUrn,
			ProjectID: tool.Tool.(*types.HTTPToolDefinition).ProjectID,
			DeploymentID: tool.Tool.(*types.HTTPToolDefinition).DeploymentID,
			Name: tool.Tool.(*types.HTTPToolDefinition).Name,
			CanonicalName: tool.Tool.(*types.HTTPToolDefinition).CanonicalName,
			Description: tool.Tool.(*types.HTTPToolDefinition).Description,
			SchemaVersion: tool.Tool.(*types.HTTPToolDefinition).SchemaVersion,
			Schema: &tool.Tool.(*types.HTTPToolDefinition).Schema,
			Confirm: tool.Tool.(*types.HTTPToolDefinition).Confirm,
			ConfirmPrompt: tool.Tool.(*types.HTTPToolDefinition).ConfirmPrompt,
			Summarizer: tool.Tool.(*types.HTTPToolDefinition).Summarizer,
			CreatedAt: tool.Tool.(*types.HTTPToolDefinition).CreatedAt,
			UpdatedAt: tool.Tool.(*types.HTTPToolDefinition).UpdatedAt,
			Canonical: tool.Tool.(*types.HTTPToolDefinition).Canonical,
			Variation: tool.Tool.(*types.HTTPToolDefinition).Variation,
		}
	case *types.PromptTemplate:
		return &types.BaseToolAttributes{
			Type: tool.Tool.(*types.PromptTemplate).Type,
			ID: tool.Tool.(*types.PromptTemplate).ID,
			ToolUrn: tool.Tool.(*types.PromptTemplate).ToolUrn,
			ProjectID: tool.Tool.(*types.PromptTemplate).ProjectID,
			DeploymentID: tool.Tool.(*types.PromptTemplate).DeploymentID,
			Name: tool.Tool.(*types.PromptTemplate).Name,
			CanonicalName: tool.Tool.(*types.PromptTemplate).CanonicalName,
			Description: tool.Tool.(*types.PromptTemplate).Description,
			SchemaVersion: tool.Tool.(*types.PromptTemplate).SchemaVersion,
			Schema: tool.Tool.(*types.PromptTemplate).Schema,
			Confirm: tool.Tool.(*types.PromptTemplate).Confirm,
			ConfirmPrompt: tool.Tool.(*types.PromptTemplate).ConfirmPrompt,
			Summarizer: tool.Tool.(*types.PromptTemplate).Summarizer,
			CreatedAt: tool.Tool.(*types.PromptTemplate).CreatedAt,
			UpdatedAt: tool.Tool.(*types.PromptTemplate).UpdatedAt,
			Canonical: tool.Tool.(*types.PromptTemplate).Canonical,
			Variation: tool.Tool.(*types.PromptTemplate).Variation,
		}
	}

	return nil
}