package conv

import (
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
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

	// Apply annotation overrides from the variation
	annotationOverrides := applyAnnotationVariation(baseTool.Annotations, variation)

	if tool.HTTPToolDefinition != nil {
		tool.HTTPToolDefinition.Name = PtrValOrEmpty(variation.Name, tool.HTTPToolDefinition.Name)
		tool.HTTPToolDefinition.Description = PtrValOrEmpty(variation.Description, tool.HTTPToolDefinition.Description)
		tool.HTTPToolDefinition.Confirm = Default(variation.Confirm, tool.HTTPToolDefinition.Confirm)
		tool.HTTPToolDefinition.ConfirmPrompt = Default(variation.ConfirmPrompt, tool.HTTPToolDefinition.ConfirmPrompt)
		tool.HTTPToolDefinition.Summarizer = Default(variation.Summarizer, tool.HTTPToolDefinition.Summarizer)
		tool.HTTPToolDefinition.Annotations = annotationOverrides

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
		tool.PromptTemplate.Annotations = annotationOverrides

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
		tool.FunctionToolDefinition.Annotations = annotationOverrides

		tool.FunctionToolDefinition.Canonical = &canonicalAttributes
		tool.FunctionToolDefinition.Variation = &variation

		if newSchema, err := variedToolSchema(baseTool.Schema, baseTool.Summarizer); err == nil {
			tool.FunctionToolDefinition.Schema = newSchema
		}
	}

	if tool.ExternalMcpToolDefinition != nil {
		tool.ExternalMcpToolDefinition.Name = PtrValOrEmpty(variation.Name, tool.ExternalMcpToolDefinition.Name)
		tool.ExternalMcpToolDefinition.Description = PtrValOrEmpty(variation.Description, tool.ExternalMcpToolDefinition.Description)
		tool.ExternalMcpToolDefinition.Confirm = Default(variation.Confirm, tool.ExternalMcpToolDefinition.Confirm)
		tool.ExternalMcpToolDefinition.ConfirmPrompt = Default(variation.ConfirmPrompt, tool.ExternalMcpToolDefinition.ConfirmPrompt)
		tool.ExternalMcpToolDefinition.Summarizer = Default(variation.Summarizer, tool.ExternalMcpToolDefinition.Summarizer)
		tool.ExternalMcpToolDefinition.Annotations = annotationOverrides

		tool.ExternalMcpToolDefinition.Canonical = &canonicalAttributes
		tool.ExternalMcpToolDefinition.Variation = &variation
	}
}

// applyAnnotationVariation merges variation overrides into the base tool's annotations.
// Variation fields that are nil are inherited from the base; non-nil values override.
func applyAnnotationVariation(base *types.ToolAnnotations, variation types.ToolVariation) *types.ToolAnnotations {
	// Start with existing annotations or an empty struct
	result := &types.ToolAnnotations{}
	if base != nil {
		result = &types.ToolAnnotations{
			Title:           base.Title,
			ReadOnlyHint:    base.ReadOnlyHint,
			DestructiveHint: base.DestructiveHint,
			IdempotentHint:  base.IdempotentHint,
			OpenWorldHint:   base.OpenWorldHint,
		}
	}

	// Apply variation overrides (nil = inherit from base)
	if variation.Title != nil {
		result.Title = variation.Title
	}
	if variation.ReadOnlyHint != nil {
		result.ReadOnlyHint = variation.ReadOnlyHint
	}
	if variation.DestructiveHint != nil {
		result.DestructiveHint = variation.DestructiveHint
	}
	if variation.IdempotentHint != nil {
		result.IdempotentHint = variation.IdempotentHint
	}
	if variation.OpenWorldHint != nil {
		result.OpenWorldHint = variation.OpenWorldHint
	}

	return result
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

// ToolAnnotations contains tool behavior hints.
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// AnnotationsFromColumns builds a *types.ToolAnnotations from individual DB boolean columns.
// Columns are nullable (pgtype.Bool) — invalid values are omitted from the result.
func AnnotationsFromColumns(readOnly, destructive, idempotent, openWorld pgtype.Bool) *types.ToolAnnotations {
	return &types.ToolAnnotations{
		ReadOnlyHint:    FromPGBool[bool](readOnly),
		DestructiveHint: FromPGBool[bool](destructive),
		IdempotentHint:  FromPGBool[bool](idempotent),
		OpenWorldHint:   FromPGBool[bool](openWorld),
	}
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

// getToolAnnotations extracts annotations from a tool's stored data.
// Annotations come from typed DB columns on the tool definition, with optional
// variation overrides applied via tool_variations (title lives on variations only).
func getToolAnnotations(tool *types.Tool) *ToolAnnotations {
	baseTool, err := ToBaseTool(tool)
	if err != nil {
		return nil
	}
	if baseTool.Annotations == nil {
		return nil
	}
	return convertStoredAnnotations(baseTool.Annotations)
}

// convertStoredAnnotations converts Goa types.ToolAnnotations to conv.ToolAnnotations.
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
		ReadOnlyHint:    stored.ReadOnlyHint,
		DestructiveHint: stored.DestructiveHint,
		IdempotentHint:  stored.IdempotentHint,
		OpenWorldHint:   stored.OpenWorldHint,
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
