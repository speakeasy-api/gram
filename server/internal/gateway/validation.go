package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/jsonschema"
)

var gramAddedFields = []string{"gram-request-summary", "environmentVariables"}

func validateToolCallBody(ctx context.Context, logger *slog.Logger, bodyBytes []byte, toolSchema string) error {
	schema, err := jsonschema.CompileSchema([]byte(toolSchema))
	if err != nil {
		logger.InfoContext(ctx, "failed to compile tool schema not moving forward with request validation", attr.SlogError(err))
		return nil
	}

	var bodyMap map[string]any
	if err := json.Unmarshal(bodyBytes, &bodyMap); err != nil {
		logger.InfoContext(ctx, "failed to parse request body not moving forward with request validation", attr.SlogError(err))
		return nil
	}

	// We need to remove gram added fields since they are not part of the tool json schema
	for _, field := range gramAddedFields {
		delete(bodyMap, field)
	}

	if err := jsonschema.ValidateAgainstSchema(schema, bodyMap); err != nil {
		return fmt.Errorf("input to toolschema validation failure: %w", err)
	}

	return nil
}
