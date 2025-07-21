package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var gramAddedFields = []string{"gram-request-summary", "environmentVariables"}

func validateToolCallBody(ctx context.Context, logger *slog.Logger, bodyBytes []byte, toolSchema string) error {
	compiler := jsonschema.NewCompiler()
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(toolSchema)))
	if err != nil {
		logger.InfoContext(ctx, "failed to parse tool schema not moving forward with request validation", slog.String("error", err.Error()))
		return nil
	}
	if err := compiler.AddResource("file:///schema.json", rawSchema); err != nil {
		logger.InfoContext(ctx, "failed to get json schema for tool schema not moving forward with request validation", slog.String("error", err.Error()))
		return nil
	}
	schema, err := compiler.Compile("file:///schema.json")
	if err != nil {
		logger.InfoContext(ctx, "failed to get json schema for tool schema not moving forward with request validation", slog.String("error", err.Error()))
		return nil
	}

	var bodyMap map[string]any
	if err := json.Unmarshal(bodyBytes, &bodyMap); err != nil {
		logger.InfoContext(ctx, "failed to parse request body not moving forward with request validation", slog.String("error", err.Error()))
		return nil
	}

	// We need to remove gram added fields since they are not part of the tool json schema
	for _, field := range gramAddedFields {
		delete(bodyMap, field)
	}

	if err := schema.Validate(bodyMap); err != nil {
		return fmt.Errorf("input to toolschema validation failure: %w", err)
	}

	return nil
}
