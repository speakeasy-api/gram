package conv

import (
	"encoding/json"
	"errors"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func GetToolURN(tool types.Tool) (*urn.Tool, error) {
	var toolURN urn.Tool

	if tool.HTTPToolDefinition != nil {
		err := toolURN.UnmarshalText([]byte(tool.HTTPToolDefinition.ToolUrn))
		if err != nil {
			return nil, urn.ErrInvalid
		}
		return &toolURN, nil
	}
	if tool.PromptTemplate != nil {
		err := toolURN.UnmarshalText([]byte(tool.PromptTemplate.ToolUrn))
		if err != nil {
			return nil, urn.ErrInvalid
		}
		return &toolURN, nil
	}
	if tool.FunctionToolDefinition != nil {
		err := toolURN.UnmarshalText([]byte(tool.FunctionToolDefinition.ToolUrn))
		if err != nil {
			return nil, urn.ErrInvalid
		}
		return &toolURN, nil
	}

	return nil, urn.ErrInvalid
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

	if tool.FunctionToolDefinition != nil {
		if len(tool.FunctionToolDefinition.Schema) > 0 {
			schema = tool.FunctionToolDefinition.Schema
		}
		return types.BaseToolAttributes{
			ID:            tool.FunctionToolDefinition.ID,
			ToolUrn:       tool.FunctionToolDefinition.ToolUrn,
			ProjectID:     tool.FunctionToolDefinition.ProjectID,
			Name:          tool.FunctionToolDefinition.Name,
			CanonicalName: tool.FunctionToolDefinition.CanonicalName,
			Description:   tool.FunctionToolDefinition.Description,
			SchemaVersion: tool.FunctionToolDefinition.SchemaVersion,
			Schema:        schema,
			Confirm:       tool.FunctionToolDefinition.Confirm,
			ConfirmPrompt: tool.FunctionToolDefinition.ConfirmPrompt,
			Summarizer:    tool.FunctionToolDefinition.Summarizer,
			CreatedAt:     tool.FunctionToolDefinition.CreatedAt,
			UpdatedAt:     tool.FunctionToolDefinition.UpdatedAt,
			Canonical:     tool.FunctionToolDefinition.Canonical,
			Variation:     tool.FunctionToolDefinition.Variation,
		}
	}

	panic(urn.ErrInvalid)
}

func ApplyVariation(tool types.Tool, variation types.ToolVariation) {
	baseTool := ToBaseTool(&tool)

	canonicalAttributes := types.CanonicalToolAttributes{
		VariationID:   variation.ID,
		Name:          baseTool.Name,
		Description:   baseTool.Description,
		Confirm:       baseTool.Confirm,
		ConfirmPrompt: baseTool.ConfirmPrompt,
		Summarizer:    baseTool.Summarizer,
	}

	if tool.HTTPToolDefinition != nil {
		tool.HTTPToolDefinition.Name = PtrValOrEmpty(variation.Name, tool.HTTPToolDefinition.Name)
		tool.HTTPToolDefinition.Description = PtrValOrEmpty(variation.Description, tool.HTTPToolDefinition.Description)
		tool.HTTPToolDefinition.Confirm = Default(variation.Confirm, tool.HTTPToolDefinition.Confirm)
		tool.HTTPToolDefinition.ConfirmPrompt = Default(variation.ConfirmPrompt, tool.HTTPToolDefinition.ConfirmPrompt)
		tool.HTTPToolDefinition.Summarizer = Default(variation.Summarizer, tool.HTTPToolDefinition.Summarizer)

		tool.HTTPToolDefinition.Canonical = &canonicalAttributes
		tool.HTTPToolDefinition.Variation = &variation

		if newSchema, err := variedToolSchema(baseTool.Schema, baseTool.Summarizer); err == nil {
			tool.HTTPToolDefinition.Schema = newSchema
		}
	}

	if tool.PromptTemplate != nil {
		tool.PromptTemplate.Name = PtrValOrEmpty(variation.Name, tool.PromptTemplate.Name)
		tool.PromptTemplate.Description = PtrValOrEmpty(variation.Description, tool.PromptTemplate.Description)
		tool.PromptTemplate.Confirm = Default(variation.Confirm, tool.PromptTemplate.Confirm)
		tool.PromptTemplate.ConfirmPrompt = Default(variation.ConfirmPrompt, tool.PromptTemplate.ConfirmPrompt)
		tool.PromptTemplate.Summarizer = Default(variation.Summarizer, tool.PromptTemplate.Summarizer)

		tool.PromptTemplate.Canonical = &canonicalAttributes
		tool.PromptTemplate.Variation = &variation

		if newSchema, err := variedToolSchema(baseTool.Schema, baseTool.Summarizer); err == nil {
			tool.PromptTemplate.Schema = newSchema
		}
	}

	if tool.FunctionToolDefinition != nil {
		tool.FunctionToolDefinition.Name = PtrValOrEmpty(variation.Name, tool.FunctionToolDefinition.Name)
		tool.FunctionToolDefinition.Description = PtrValOrEmpty(variation.Description, tool.FunctionToolDefinition.Description)
		tool.FunctionToolDefinition.Confirm = Default(variation.Confirm, tool.FunctionToolDefinition.Confirm)
		tool.FunctionToolDefinition.ConfirmPrompt = Default(variation.ConfirmPrompt, tool.FunctionToolDefinition.ConfirmPrompt)
		tool.FunctionToolDefinition.Summarizer = Default(variation.Summarizer, tool.FunctionToolDefinition.Summarizer)

		tool.FunctionToolDefinition.Canonical = &canonicalAttributes
		tool.FunctionToolDefinition.Variation = &variation

		if newSchema, err := variedToolSchema(baseTool.Schema, baseTool.Summarizer); err == nil {
			tool.FunctionToolDefinition.Schema = newSchema
		}
	}
}

func variedToolSchema(baseSchema string, summarizer *string) (string, error) {
	schema := baseSchema
	if summarizer != nil {
		var jsonSchema map[string]interface{}
		err := json.Unmarshal([]byte(schema), &jsonSchema)
		if err != nil {
			return "", errors.New("failed to unmarshal schema")
		}

		properties, ok := jsonSchema["properties"].(map[string]interface{})
		if !ok {
			properties = make(map[string]interface{})
		}

		properties["gram-request-summary"] = map[string]interface{}{
			"type":        "string",
			"description": "REQUIRED: A summary of the request to the tool. Distill the user's intention in order to ensure the response contains all the necessary information, without unnecessary details.",
		}

		jsonSchema["properties"] = properties

		var required []string
		required, ok = jsonSchema["required"].([]string)
		if !ok {
			required = []string{}
		}

		required = append(required, "gram-request-summary")
		jsonSchema["required"] = required

		newSchema, err := json.Marshal(jsonSchema)
		if err != nil {
			return "", errors.New("failed to marshal schema")
		}

		schema = string(newSchema)
	}

	return schema, nil
}
