package templates

import (
	"fmt"
	"io"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type jsonSchemaValidationError string

var (
	errSchemaHasNoProperties = jsonSchemaValidationError("schema has no properties or additionalProperties defined")
	errSchemaUnsupportedType = jsonSchemaValidationError("schema type is not supported")
	errSchemaNotObject       = jsonSchemaValidationError("schema type must be object")
)

func (e jsonSchemaValidationError) Error() string {
	return string(e)
}

func validateInputSchema(rawInput io.Reader) error {
	rawSchema, err := jsonschema.UnmarshalJSON(rawInput)
	if err != nil {
		return fmt.Errorf("unmarshal schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("file:///schema.json", rawSchema); err != nil {
		return fmt.Errorf("add schema: %w", err)
	}

	schema, err := compiler.Compile("file:///schema.json")
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}

	if schema.Types != nil {
		types := schema.Types.ToStrings()
		hasObject := false
		isUnrecognizedType := false

		for _, t := range types {
			switch t {
			case "object":
				hasObject = true
			case "null":
				// Allowing arguments top level object to be nullable for now.
				// If this proves to be a problem, we show remove this branch.
				continue
			default:
				isUnrecognizedType = true
			}
		}

		if isUnrecognizedType {
			return fmt.Errorf("%w: %v", errSchemaUnsupportedType, types)
		}

		if !hasObject {
			return fmt.Errorf("%w: %v", errSchemaNotObject, types)
		}
	}

	if len(schema.Properties) == 0 && !hasAdditionalProperties(schema) {
		return errSchemaHasNoProperties
	}

	return nil
}

func hasAdditionalProperties(schema *jsonschema.Schema) bool {
	switch v := schema.AdditionalProperties.(type) {
	case bool:
		return v
	case *jsonschema.Schema:
		return true
	default:
		return false
	}
}
