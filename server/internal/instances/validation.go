package instances

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func ValidateToolCallBody(ctx context.Context, bodyBytes []byte, toolSchema string) error {
	compiler := jsonschema.NewCompiler()
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(toolSchema)))
	if err != nil {
		return fmt.Errorf("failed to parse tool schema: %w", err)
	}
	if err := compiler.AddResource("file:///schema.json", rawSchema); err != nil {
		return err
	}
	schema, err := compiler.Compile("file:///schema.json")
	if err != nil {
		return fmt.Errorf("failed to compile tool schema: %w", err)
	}

	var bodyMap any
	if err := json.Unmarshal(bodyBytes, &bodyMap); err != nil {
		return fmt.Errorf("failed to parse request body: %w", err)
	}

	if err := schema.Validate(bodyMap); err != nil {
		return fmt.Errorf("failed to validate tool call body: %w", err)
	}

	return nil
}
