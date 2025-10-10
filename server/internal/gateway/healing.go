package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

func validateAndAttemptHealing(ctx context.Context, logger *slog.Logger, bodyBytes []byte, toolSchema string) (json.RawMessage, error) {
	err := validateToolCallBody(ctx, logger, bodyBytes, toolSchema)
	if err == nil { // Validation passed, return the body as is
		return bodyBytes, nil
	}

	// Validation failed, attempt to heal the body by recursively parsing stringified JSON
	if len(bodyBytes) == 0 {
		return bodyBytes, err
	}

	healed, didHeal := healStringifiedJSON(ctx, logger, bodyBytes, toolSchema)
	if didHeal {
		// Re-validate after healing
		if err = validateToolCallBody(ctx, logger, healed, toolSchema); err == nil {
			logger.InfoContext(ctx, "successfully healed stringified JSON body")
			return healed, nil
		}
	}

	return bodyBytes, err
}

// healStringifiedJSON recursively attempts to parse stringified JSON in a body,
// using the JSON schema to identify where objects are expected but strings are provided.
// Returns the healed body and a boolean indicating if any healing was performed.
func healStringifiedJSON(ctx context.Context, logger *slog.Logger, body json.RawMessage, schemaStr string) (json.RawMessage, bool) {
	// Parse the body as a generic value (could be object, string, array, etc.)
	var bodyValue any
	if err := json.Unmarshal(body, &bodyValue); err != nil {
		// Not valid JSON, can't heal
		return body, false
	}

	// Parse the schema
	var schemaMap map[string]any
	if err := json.Unmarshal([]byte(schemaStr), &schemaMap); err != nil {
		logger.WarnContext(ctx, "failed to parse schema for healing", attr.SlogError(err))
		return body, false
	}

	// Recursively heal the body based on the schema
	healed := healValue(bodyValue, schemaMap)

	// Marshal the healed body back to JSON
	healedBytes, err := json.Marshal(healed)
	if err != nil {
		logger.WarnContext(ctx, "failed to marshal healed body", attr.SlogError(err))
		return body, false
	}

	// Check if anything changed
	changed := !bytes.Equal(body, healedBytes)
	return json.RawMessage(healedBytes), changed
}

// healValue recursively heals a value based on its schema definition.
// If the value is a string, attempt to parse it as JSON first.
func healValue(value any, schema any) any {
	// Step 1: If value is a string, try to parse it as JSON
	if strVal, isString := value.(string); isString {
		var parsed any
		if err := json.Unmarshal([]byte(strVal), &parsed); err == nil {
			// Successfully parsed, continue healing with the parsed value
			value = parsed
		} else {
			// If the value is a string but not valid JSON, there's nothing more to check
			return value
		}
	}

	// Step 2: If value is an object, recursively heal its properties
	valueMap, isMap := value.(map[string]any)
	if !isMap {
		// Not a map - check if it's an array
		if arr, isArray := value.([]any); isArray {
			// Heal array items if schema is available
			schemaMap, ok := schema.(map[string]any)
			if ok {
				if items, hasItems := schemaMap["items"]; hasItems {
					healed := make([]any, len(arr))
					for i, item := range arr {
						healed[i] = healValue(item, items)
					}
					return healed
				}
			}
		}
		// For primitives or arrays without schema, return as-is
		return value
	}

	// Step 3: Extract properties schema from the schema definition
	// Handle various schema formats: type:object, oneOf, anyOf, or direct properties
	var propertiesSchema map[string]any

	if schemaMap, ok := schema.(map[string]any); ok {
		// Try direct properties
		if props, ok := schemaMap["properties"].(map[string]any); ok {
			propertiesSchema = props
		} else if oneOf, ok := schemaMap["oneOf"].([]any); ok {
			// For oneOf/anyOf, use the first option with properties
			for _, option := range oneOf {
				if optMap, ok := option.(map[string]any); ok {
					if props, ok := optMap["properties"].(map[string]any); ok {
						propertiesSchema = props
						break
					}
				}
			}
		} else if anyOf, ok := schemaMap["anyOf"].([]any); ok {
			for _, option := range anyOf {
				if optMap, ok := option.(map[string]any); ok {
					if props, ok := optMap["properties"].(map[string]any); ok {
						propertiesSchema = props
						break
					}
				}
			}
		}
	}

	// Step 4: Recursively heal each property in the object
	if propertiesSchema != nil {
		healed := make(map[string]any)
		for key, val := range valueMap {
			if propSchema, ok := propertiesSchema[key]; ok {
				healed[key] = healValue(val, propSchema)
			} else {
				healed[key] = val
			}
		}
		return healed
	}

	// No schema found, return the value as-is
	return valueMap
}
