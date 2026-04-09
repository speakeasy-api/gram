package core

import (
	"encoding/json"
	"fmt"
	"reflect"

	gjsonschema "github.com/google/jsonschema-go/jsonschema"
)

type InputSchemaOption func(*inputSchemaConfig)

type inputSchemaConfig struct {
	forOptions       *gjsonschema.ForOptions
	propertyMutators map[string][]func(*gjsonschema.Schema)
}

func BuildInputSchema[T any](options ...InputSchemaOption) []byte {
	config := &inputSchemaConfig{
		forOptions: &gjsonschema.ForOptions{
			IgnoreInvalidTypes: false,
			TypeSchemas:        map[reflect.Type]*gjsonschema.Schema{},
		},
		propertyMutators: map[string][]func(*gjsonschema.Schema){},
	}

	for _, option := range options {
		option(config)
	}

	schema, err := gjsonschema.For[T](config.forOptions)
	if err != nil {
		panic(fmt.Errorf("build input schema: %w", err))
	}

	for propertyName, mutators := range config.propertyMutators {
		prop := schema.Properties[propertyName]
		if prop == nil {
			continue
		}
		for _, mutate := range mutators {
			mutate(prop)
		}
	}

	return mustMarshalJSON(schema)
}

func WithTypeSchema(schemaType reflect.Type, schema *gjsonschema.Schema) InputSchemaOption {
	return func(config *inputSchemaConfig) {
		config.forOptions.TypeSchemas[schemaType] = schema
	}
}

func WithPropertyMutator(propertyName string, mutate func(*gjsonschema.Schema)) InputSchemaOption {
	return func(config *inputSchemaConfig) {
		config.propertyMutators[propertyName] = append(config.propertyMutators[propertyName], mutate)
	}
}

func WithPropertyFormat(propertyName string, format string) InputSchemaOption {
	return WithPropertyMutator(propertyName, func(prop *gjsonschema.Schema) {
		prop.Format = format
	})
}

func WithPropertyEnum(propertyName string, values ...any) InputSchemaOption {
	return WithPropertyMutator(propertyName, func(prop *gjsonschema.Schema) {
		prop.Enum = values
	})
}

func WithPropertyNumberRange(propertyName string, minValue float64, maxValue float64) InputSchemaOption {
	return WithPropertyMutator(propertyName, func(prop *gjsonschema.Schema) {
		prop.Minimum = &minValue
		prop.Maximum = &maxValue
	})
}

func PermissiveObjectSchema() *gjsonschema.Schema {
	//nolint:exhaustruct // zero values are the intended defaults for omitted schema fields
	return &gjsonschema.Schema{
		Type:                 "object",
		AdditionalProperties: &gjsonschema.Schema{},
	}
}

func mustMarshalJSON(v any) []byte {
	bs, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("marshal schema: %w", err))
	}
	return bs
}
