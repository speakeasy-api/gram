// Package contract owns the offline registry schema and native JSON Schema validation.
package contract

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed record.schema.json
var Schema []byte

func decode(raw []byte) (any, error) {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode registry contract JSON: %w", err)
	}
	return value, nil
}

// Compile compiles the offline contract with the validator's built-in formats.
func Compile(raw []byte) (*jsonschema.Schema, error) {
	value, err := decode(raw)
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource("record.schema.json", value); err != nil {
		return nil, fmt.Errorf("add registry contract resource: %w", err)
	}
	schema, err := c.Compile("record.schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile registry contract schema: %w", err)
	}
	return schema, nil
}
