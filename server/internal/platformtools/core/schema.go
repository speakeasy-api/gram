package core

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type InputSchemaOption func(*inputSchemaConfig)

type inputSchemaConfig struct {
	forOptions       *jsonschema.ForOptions
	propertyMutators map[string][]func(*jsonschema.Schema)
}

func BuildInputSchema[T any](options ...InputSchemaOption) []byte {
	config := &inputSchemaConfig{
		forOptions: &jsonschema.ForOptions{
			IgnoreInvalidTypes: false,
			TypeSchemas:        map[reflect.Type]*jsonschema.Schema{},
		},
		propertyMutators: map[string][]func(*jsonschema.Schema){},
	}

	for _, option := range options {
		option(config)
	}

	schema, err := jsonschema.For[T](config.forOptions)
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

func WithTypeSchema(schemaType reflect.Type, schema *jsonschema.Schema) InputSchemaOption {
	return func(config *inputSchemaConfig) {
		config.forOptions.TypeSchemas[schemaType] = schema
	}
}

func WithPropertyMutator(propertyName string, mutate func(*jsonschema.Schema)) InputSchemaOption {
	return func(config *inputSchemaConfig) {
		config.propertyMutators[propertyName] = append(config.propertyMutators[propertyName], mutate)
	}
}

func WithPropertyFormat(propertyName string, format string) InputSchemaOption {
	return WithPropertyMutator(propertyName, func(prop *jsonschema.Schema) {
		prop.Format = format
	})
}

func WithPropertyEnum(propertyName string, values ...any) InputSchemaOption {
	return WithPropertyMutator(propertyName, func(prop *jsonschema.Schema) {
		prop.Enum = values
	})
}

func WithPropertyNumberRange(propertyName string, minValue float64, maxValue float64) InputSchemaOption {
	return WithPropertyMutator(propertyName, func(prop *jsonschema.Schema) {
		prop.Minimum = &minValue
		prop.Maximum = &maxValue
	})
}

func PermissiveObjectSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:                 "object",
		AdditionalProperties: &jsonschema.Schema{},
	}
}

func mustMarshalJSON(v any) []byte {
	bs, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("marshal schema: %w", err))
	}
	return bs
}
