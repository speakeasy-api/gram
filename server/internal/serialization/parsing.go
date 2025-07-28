package serialization

import (
	"context"
	"log/slog"
	"net/url"
	"reflect"
)

// HTTPParameter holds the settings for encoding a parameter into an HTTP
// request.
//
// Note: this struct is a duplicate of one in the gateway package as a temporary
// measure. The goal is to merge these two packages in the future.
type HTTPParameter struct {
	Name            string `json:"name" yaml:"name"`
	Style           string `json:"style" yaml:"style"`
	Explode         *bool  `json:"explode" yaml:"explode"`
	AllowEmptyValue bool   `json:"allow_empty_value" yaml:"allow_empty_value"`
}

// ParsePathAndHeaderParameter parses path and header parameters.
// We currently only support simple style for these parameter types.
func ParsePathAndHeaderParameter(ctx context.Context, logger *slog.Logger, parentName string, objType reflect.Type, objValue reflect.Value, parameterSettings *HTTPParameter) map[string]string {
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
func ParseQueryParameter(ctx context.Context, logger *slog.Logger, parentName string, objType reflect.Type, objValue reflect.Value, parameterSettings *HTTPParameter) url.Values {
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
