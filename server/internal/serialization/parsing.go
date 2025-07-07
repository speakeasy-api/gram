package serialization

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"

	"github.com/speakeasy-api/gram/server/internal/openapi"
)

func ParseParameterSettings(settings []byte) (map[string]*openapi.OpenapiV3ParameterProxy, error) {
	result := make(map[string]*openapi.OpenapiV3ParameterProxy)
	if len(settings) == 0 {
		return result, nil
	}

	if err := json.Unmarshal(settings, &result); err != nil {
		return nil, fmt.Errorf("parse parameter settings: %w", err)
	}

	return result, nil
}

// ParsePathAndHeaderParameter parses path and header parameters.
// We currently only support simple style for these parameter types.
func ParsePathAndHeaderParameter(ctx context.Context, logger *slog.Logger, parentName string, objType reflect.Type, objValue reflect.Value, parameterSettings *openapi.OpenapiV3ParameterProxy) map[string]string {
	style := "simple"
	explode := setExplodeDefaults(style)
	if parameterSettings != nil && parameterSettings.Style != "" && parameterSettings.Style != "simple" {
		logger.WarnContext(ctx, "unsupported style for path and header parameters", slog.String("style", parameterSettings.Style))
	}
	if parameterSettings != nil && parameterSettings.Explode != nil {
		explode = *parameterSettings.Explode
	}
	return parseSimpleParams(parentName, objType, objValue, explode)
}

// ParseQueryParameter parses query parameters based on the style and explode flag.
func ParseQueryParameter(ctx context.Context, logger *slog.Logger, parentName string, objType reflect.Type, objValue reflect.Value, parameterSettings *openapi.OpenapiV3ParameterProxy) url.Values {
	style := "form" // Default style for query parameters
	if parameterSettings != nil && parameterSettings.Style != "" {
		style = parameterSettings.Style
	}
	explode := setExplodeDefaults(style)
	if parameterSettings != nil && parameterSettings.Explode != nil {
		explode = *parameterSettings.Explode
	}
	switch style {
	case "form":
		return parseFormParams(parentName, objType, objValue, ",", explode)
	case "spaceDelimited":
		return parseFormParams(parentName, objType, objValue, " ", explode)
	case "pipeDelimited":
		return parseFormParams(parentName, objType, objValue, "|", explode)
	case "deepObject":
		return parseDeepObjectParams(parentName, objType, objValue)
	default:
		logger.WarnContext(ctx, "unsupported style for query parameters", slog.String("style", style))
		return parseFormParams(parentName, objType, objValue, ",", true) // defaulting back to form style
	}
}

func setExplodeDefaults(style string) bool {
	switch style {
	case "form":
		return true
	default:
		return false
	}
}
