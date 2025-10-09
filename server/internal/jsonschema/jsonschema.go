package jsonschema

import (
	"bytes"
	"fmt"
	"io"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// schemaResourceURI is the internal URI used for schema compilation
	schemaResourceURI = "file:///schema.json"
)

// ValidationError represents a JSON schema validation error type.
type ValidationError string

const (
	ErrSchemaHasNoProperties ValidationError = "schema has no properties or additionalProperties defined"
	ErrSchemaUnsupportedType ValidationError = "schema type is not supported"
	ErrSchemaNotObject       ValidationError = "schema type must be object"
)

func (e ValidationError) Error() string {
	return string(e)
}

// IsValidJSONSchema validates that the provided bytes represent a valid JSON schema.
func IsValidJSONSchema(bs []byte) error {
	compiler := jsonschema.NewCompiler()
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(bs))
	if err != nil {
		return fmt.Errorf("parse json schema bytes: %w", err)
	}
	if err = compiler.AddResource(schemaResourceURI, rawSchema); err != nil {
		return fmt.Errorf("add json schema resource: %w", err)
	}
	if _, err = compiler.Compile(schemaResourceURI); err != nil {
		fmt.Print("\n\n\n\n\n")
		fmt.Println(string(bs))
		return fmt.Errorf("compile json schema: %w", err)
	}
	return nil
}

// CompileSchema compiles a JSON schema from bytes and returns the compiled schema.
func CompileSchema(schemaBytes []byte) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("parse json schema: %w", err)
	}
	if err = compiler.AddResource(schemaResourceURI, rawSchema); err != nil {
		return nil, fmt.Errorf("add schema resource: %w", err)
	}
	schema, err := compiler.Compile(schemaResourceURI)
	if err != nil {
		fmt.Print("\n\n\n\n\n")
		fmt.Println(string(schemaBytes))
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	return schema, nil
}

// ValidateAgainstSchema validates data against a compiled JSON schema.
func ValidateAgainstSchema(schema *jsonschema.Schema, data any) error {
	if err := schema.Validate(data); err != nil {
		return fmt.Errorf("validation failure: %w", err)
	}
	return nil
}

// ValidateInputSchema validates that an input schema is a valid object-type schema
// with properties or additionalProperties defined. This is specifically for
// validating schemas used as tool/template input definitions.
func ValidateInputSchema(rawInput io.Reader) error {
	rawSchema, err := jsonschema.UnmarshalJSON(rawInput)
	if err != nil {
		return fmt.Errorf("unmarshal schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err = compiler.AddResource(schemaResourceURI, rawSchema); err != nil {
		return fmt.Errorf("add schema: %w", err)
	}

	schema, err := compiler.Compile(schemaResourceURI)
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
				// If this proves to be a problem, we should remove this branch.
				continue
			default:
				isUnrecognizedType = true
			}
		}

		if isUnrecognizedType {
			return fmt.Errorf("%w: %v", ErrSchemaUnsupportedType, types)
		}

		if !hasObject {
			return fmt.Errorf("%w: %v", ErrSchemaNotObject, types)
		}
	}

	if len(schema.Properties) == 0 && !hasAdditionalProperties(schema) {
		return ErrSchemaHasNoProperties
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
