package conv

import (
	"encoding/json"
	"errors"
	"strings"

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
	if tool.ExternalMcpToolDefinition != nil {
		err := toolURN.UnmarshalText([]byte(tool.ExternalMcpToolDefinition.ToolUrn))
		if err != nil {
			return nil, urn.ErrInvalid
		}
		return &toolURN, nil
	}

	return nil, urn.ErrInvalid
}

func IsProxyTool(tool *types.Tool) bool {
	return tool != nil && tool.ExternalMcpToolDefinition != nil && tool.ExternalMcpToolDefinition.Type != nil && *tool.ExternalMcpToolDefinition.Type == "proxy"
}

func ToBaseTool(tool *types.Tool) (types.BaseToolAttributes, error) {
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
			Annotations:   tool.HTTPToolDefinition.Annotations,
		}, nil
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
			Annotations:   tool.PromptTemplate.Annotations,
		}, nil
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
			Annotations:   tool.FunctionToolDefinition.Annotations,
		}, nil
	}

	if tool.ExternalMcpToolDefinition != nil {
		if len(tool.ExternalMcpToolDefinition.Schema) > 0 {
			schema = tool.ExternalMcpToolDefinition.Schema
		}
		return types.BaseToolAttributes{
			ID:            tool.ExternalMcpToolDefinition.ID,
			ToolUrn:       tool.ExternalMcpToolDefinition.ToolUrn,
			ProjectID:     tool.ExternalMcpToolDefinition.ProjectID,
			Name:          tool.ExternalMcpToolDefinition.Name,
			CanonicalName: tool.ExternalMcpToolDefinition.CanonicalName,
			Description:   tool.ExternalMcpToolDefinition.Description,
			SchemaVersion: tool.ExternalMcpToolDefinition.SchemaVersion,
			Schema:        schema,
			CreatedAt:     tool.ExternalMcpToolDefinition.CreatedAt,
			UpdatedAt:     tool.ExternalMcpToolDefinition.UpdatedAt,
			Confirm:       nil,
			ConfirmPrompt: nil,
			Summarizer:    nil,
			Canonical:     nil,
			Variation:     nil,
			Annotations:   tool.ExternalMcpToolDefinition.Annotations,
		}, nil
	}

	return types.BaseToolAttributes{}, urn.ErrInvalid
}

func ApplyVariation(tool types.Tool, variation types.ToolVariation) {
	baseTool, err := ToBaseTool(&tool)
	if err != nil {
		panic("ApplyVariation called with unsupported tool type: " + err.Error())
	}

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

// ToolAnnotations contains MCP tool behavior hints.
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// ToToolListEntry converts a Tool to basic list entry fields.
// ToolListEntry contains the fields needed for a tool list entry.
type ToolListEntry struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Meta        map[string]any
	Annotations *ToolAnnotations
}

// ToToolListEntry extracts tool list entry fields from a tool.
// Returns error for proxy tools that must be unfolded first.
func ToToolListEntry(tool *types.Tool) (ToolListEntry, error) {
	if tool == nil {
		return ToolListEntry{
			Name:        "",
			Description: "",
			InputSchema: nil,
			Meta:        nil,
			Annotations: nil,
		}, nil
	}

	var meta map[string]any
	if tool.FunctionToolDefinition != nil {
		meta = tool.FunctionToolDefinition.Meta
	}

	baseTool, err := ToBaseTool(tool)
	if err != nil {
		return ToolListEntry{}, err
	}

	// Build annotations - start with stored annotations or infer from HTTP method
	annotations := getToolAnnotations(tool)

	return ToolListEntry{
		Name:        baseTool.Name,
		Description: baseTool.Description,
		InputSchema: json.RawMessage(baseTool.Schema),
		Meta:        meta,
		Annotations: annotations,
	}, nil
}

// getToolAnnotations extracts or infers annotations for a tool.
// For HTTP tools without explicit annotations, infers from HTTP method.
func getToolAnnotations(tool *types.Tool) *ToolAnnotations {
	// Check for stored annotations first
	if tool.HTTPToolDefinition != nil && tool.HTTPToolDefinition.Annotations != nil {
		return convertStoredAnnotations(tool.HTTPToolDefinition.Annotations)
	}
	if tool.FunctionToolDefinition != nil && tool.FunctionToolDefinition.Annotations != nil {
		return convertStoredAnnotations(tool.FunctionToolDefinition.Annotations)
	}

	// For HTTP tools without explicit annotations, infer from HTTP method
	if tool.HTTPToolDefinition != nil && tool.HTTPToolDefinition.HTTPMethod != "" {
		return inferAnnotationsFromHTTPMethod(tool.HTTPToolDefinition.HTTPMethod)
	}

	return nil
}

// convertStoredAnnotations converts stored types.ToolAnnotations to conv.ToolAnnotations.
func convertStoredAnnotations(stored *types.ToolAnnotations) *ToolAnnotations {
	if stored == nil {
		return nil
	}
	var title string
	if stored.Title != nil {
		title = *stored.Title
	}
	return &ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    &stored.ReadOnlyHint,
		DestructiveHint: &stored.DestructiveHint,
		IdempotentHint:  &stored.IdempotentHint,
		OpenWorldHint:   &stored.OpenWorldHint,
	}
}

// inferAnnotationsFromHTTPMethod returns inferred annotations based on HTTP method semantics.
func inferAnnotationsFromHTTPMethod(method string) *ToolAnnotations {
	t := true
	f := false

	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS":
		// Read-only methods
		return &ToolAnnotations{
			Title:           "",
			ReadOnlyHint:    &t,
			DestructiveHint: &f,
			IdempotentHint:  &t,
			OpenWorldHint:   nil,
		}
	case "PUT":
		// Idempotent write
		return &ToolAnnotations{
			Title:           "",
			ReadOnlyHint:    &f,
			DestructiveHint: &f,
			IdempotentHint:  &t,
			OpenWorldHint:   nil,
		}
	case "DELETE":
		// Destructive and idempotent
		return &ToolAnnotations{
			Title:           "",
			ReadOnlyHint:    &f,
			DestructiveHint: &t,
			IdempotentHint:  &t,
			OpenWorldHint:   nil,
		}
	case "POST", "PATCH":
		// Non-idempotent writes
		return &ToolAnnotations{
			Title:           "",
			ReadOnlyHint:    &f,
			DestructiveHint: &f,
			IdempotentHint:  &f,
			OpenWorldHint:   nil,
		}
	default:
		return nil
	}
}

// ResolvedExternalTool contains the tool metadata resolved from an external MCP server.
type ResolvedExternalTool struct {
	Name        string
	Description string
	Schema      string
}

// ProxyToolToBaseTool converts an unfolded external MCP tool to base attributes.
// Takes the proxy tool definition and the resolved tool metadata from the MCP server.
func ProxyToolToBaseTool(proxy *types.ExternalMCPToolDefinition, resolved ResolvedExternalTool) types.BaseToolAttributes {
	schema := resolved.Schema
	if schema == "" {
		schema = constants.DefaultEmptyToolSchema
	}

	// Prefix the tool name with the slug to namespace it
	prefixedName := proxy.Slug + ":" + resolved.Name

	return types.BaseToolAttributes{
		ID:            proxy.ID,
		ToolUrn:       proxy.ToolUrn,
		ProjectID:     "",
		Name:          prefixedName,
		CanonicalName: prefixedName,
		Description:   resolved.Description,
		SchemaVersion: nil,
		Schema:        schema,
		Confirm:       nil,
		ConfirmPrompt: nil,
		Summarizer:    nil,
		CreatedAt:     proxy.CreatedAt,
		UpdatedAt:     proxy.UpdatedAt,
		Canonical:     nil,
		Variation:     nil,
		Annotations:   nil,
	}
}
