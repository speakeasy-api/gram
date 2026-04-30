package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	risk_repo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// xGramToolsetIDPropName is the JSON-schema property the MCP server injects
// into every Gram-hosted tool's input schema (see injectToolsetIDConstant) and
// that tool callers must echo back so the hooks layer can validate the call.
// Strip it from the call payload before forwarding to the underlying tool.
const xGramToolsetIDPropName = "x-gram-toolset-id"

// hasEnabledShadowMCPPolicy reports whether the project has at least one
// enabled shadow_mcp risk policy (any action). This drives whether the MCP
// server injects the x-gram-toolset-id constant into tool input schemas.
// A lookup failure returns false so schema injection stays off rather than
// breaking otherwise-valid tool calls.
func hasEnabledShadowMCPPolicy(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	projectID uuid.UUID,
) bool {
	if projectID == uuid.Nil {
		return false
	}
	policies, err := risk_repo.New(db).ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		logger.WarnContext(ctx, "failed to list shadow_mcp policies; defaulting to off",
			attr.SlogError(err),
		)
		return false
	}
	return len(policies) > 0
}

// injectToolsetIDConstant injects an "x-gram-toolset-id" property into the tool's
// input JSON schema as a required const equal to the toolset ID. Tool callers must
// echo this value back, allowing downstream handlers to recover the originating
// toolset from arbitrary tool-call payloads.
func injectToolsetIDConstant(schema json.RawMessage, toolsetID string) (json.RawMessage, error) {
	const fieldName = xGramToolsetIDPropName

	schemaMap := map[string]any{}
	if len(schema) > 0 {
		if err := json.Unmarshal(schema, &schemaMap); err != nil {
			return schema, oops.E(oops.CodeUnexpected, err, "failed to parse tool input schema")
		}
	}

	if _, ok := schemaMap["type"]; !ok {
		schemaMap["type"] = "object"
	}

	props, _ := schemaMap["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	props[fieldName] = map[string]any{
		"type":        "string",
		"const":       toolsetID,
		"description": "Internal Gram toolset identifier. Must be passed through unchanged.",
	}
	schemaMap["properties"] = props

	required, _ := schemaMap["required"].([]any)
	hasField := false
	for _, r := range required {
		if s, ok := r.(string); ok && s == fieldName {
			hasField = true
			break
		}
	}
	if !hasField {
		required = append(required, fieldName)
	}
	schemaMap["required"] = required

	out, err := json.Marshal(schemaMap)
	if err != nil {
		return schema, oops.E(oops.CodeUnexpected, err, "failed to marshal tool input schema")
	}
	return out, nil
}

// stripGramToolsetIDProperty removes the x-gram-toolset-id property from a
// tool-call arguments JSON object. Returns the input unchanged when the
// arguments are empty, not a JSON object, or don't contain the property.
func stripGramToolsetIDProperty(args json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return args, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return args, fmt.Errorf("unmarshal tool arguments: %w", err)
	}
	if _, ok := obj[xGramToolsetIDPropName]; !ok {
		return args, nil
	}
	delete(obj, xGramToolsetIDPropName)

	out, err := json.Marshal(obj)
	if err != nil {
		return args, fmt.Errorf("marshal tool arguments: %w", err)
	}
	return out, nil
}
