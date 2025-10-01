package jsonschema

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsValidJSONSchema_ValidSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema []byte
	}{
		{
			name: "simple object schema",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"age": {"type": "integer"}
				}
			}`),
		},
		{
			name: "array schema",
			schema: []byte(`{
				"type": "array",
				"items": {"type": "string"}
			}`),
		},
		{
			name: "nested schema",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"user": {
						"type": "object",
						"properties": {
							"name": {"type": "string"}
						}
					}
				}
			}`),
		},
		{
			name: "schema with required fields",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"email": {"type": "string"}
				},
				"required": ["email"]
			}`),
		},
		{
			name: "empty object schema",
			schema: []byte(`{
				"type": "object"
			}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := IsValidJSONSchema(tt.schema)
			require.NoError(t, err)
		})
	}
}

func TestIsValidJSONSchema_InvalidSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		schema      []byte
		expectedErr string
	}{
		{
			name:        "malformed JSON",
			schema:      []byte(`{invalid json`),
			expectedErr: "parse json schema bytes",
		},
		{
			name:        "null schema",
			schema:      []byte(`null`),
			expectedErr: "compile json schema",
		},
		{
			name: "invalid type value",
			schema: []byte(`{
				"type": "invalid_type"
			}`),
			expectedErr: "compile json schema",
		},
		{
			name: "invalid property definition",
			schema: []byte(`{
				"type": "object",
				"properties": "not an object"
			}`),
			expectedErr: "compile json schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := IsValidJSONSchema(tt.schema)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestCompileSchema_Success(t *testing.T) {
	t.Parallel()

	schema := []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		}
	}`)

	compiled, err := CompileSchema(schema)
	require.NoError(t, err)
	require.NotNil(t, compiled)
}

func TestCompileSchema_InvalidJSON(t *testing.T) {
	t.Parallel()

	schema := []byte(`{invalid}`)

	compiled, err := CompileSchema(schema)
	require.Error(t, err)
	require.Nil(t, compiled)
	require.Contains(t, err.Error(), "parse json schema")
}

func TestCompileSchema_InvalidSchema(t *testing.T) {
	t.Parallel()

	schema := []byte(`{
		"type": "object",
		"properties": {
			"field": {"type": "nonexistent_type"}
		}
	}`)

	compiled, err := CompileSchema(schema)
	require.Error(t, err)
	require.Nil(t, compiled)
	require.Contains(t, err.Error(), "compile schema")
}

func TestValidateAgainstSchema_Valid(t *testing.T) {
	t.Parallel()

	schemaBytes := []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "integer"}
		},
		"required": ["name"]
	}`)

	schema, err := CompileSchema(schemaBytes)
	require.NoError(t, err)

	data := map[string]any{
		"name": "John Doe",
		"age":  30,
	}

	err = ValidateAgainstSchema(schema, data)
	require.NoError(t, err)
}

func TestValidateAgainstSchema_Invalid(t *testing.T) {
	t.Parallel()

	schemaBytes := []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "integer"}
		},
		"required": ["name"]
	}`)

	schema, err := CompileSchema(schemaBytes)
	require.NoError(t, err)

	tests := []struct {
		name string
		data any
	}{
		{
			name: "missing required field",
			data: map[string]any{
				"age": 30,
			},
		},
		{
			name: "wrong type",
			data: map[string]any{
				"name": 123,
			},
		},
		{
			name: "not an object",
			data: "string value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAgainstSchema(schema, tt.data)
			require.Error(t, err)
			require.Contains(t, err.Error(), "validation failure")
		})
	}
}

func TestValidateInputSchema_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
	}{
		{
			name: "object with properties",
			schema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}`,
		},
		{
			name: "object with additionalProperties",
			schema: `{
				"type": "object",
				"additionalProperties": true
			}`,
		},
		{
			name: "object with additionalProperties schema",
			schema: `{
				"type": "object",
				"additionalProperties": {"type": "string"}
			}`,
		},
		{
			name: "nullable object with properties",
			schema: `{
				"type": ["object", "null"],
				"properties": {
					"field": {"type": "string"}
				}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader := bytes.NewReader([]byte(tt.schema))
			err := ValidateInputSchema(reader)
			require.NoError(t, err)
		})
	}
}

func TestValidateInputSchema_Invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		schema      string
		expectedErr ValidationError
	}{
		{
			name: "object with no properties",
			schema: `{
				"type": "object"
			}`,
			expectedErr: ErrSchemaHasNoProperties,
		},
		{
			name: "string type",
			schema: `{
				"type": "string"
			}`,
			expectedErr: ErrSchemaUnsupportedType,
		},
		{
			name: "array type",
			schema: `{
				"type": "array",
				"items": {"type": "string"}
			}`,
			expectedErr: ErrSchemaUnsupportedType,
		},
		{
			name: "integer type",
			schema: `{
				"type": "integer"
			}`,
			expectedErr: ErrSchemaUnsupportedType,
		},
		{
			name: "boolean type",
			schema: `{
				"type": "boolean"
			}`,
			expectedErr: ErrSchemaUnsupportedType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader := bytes.NewReader([]byte(tt.schema))
			err := ValidateInputSchema(reader)
			require.Error(t, err)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestValidateInputSchema_MalformedJSON(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader([]byte(`{invalid json}`))
	err := ValidateInputSchema(reader)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal schema")
}

func TestValidateInputSchema_InvalidSchemaStructure(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader([]byte(`{
		"type": "object",
		"properties": {
			"field": {"type": "invalid_type_here"}
		}
	}`))
	err := ValidateInputSchema(reader)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compile schema")
}

func TestValidationError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      ValidationError
		expected string
	}{
		{
			name:     "ErrSchemaHasNoProperties",
			err:      ErrSchemaHasNoProperties,
			expected: "schema has no properties or additionalProperties defined",
		},
		{
			name:     "ErrSchemaUnsupportedType",
			err:      ErrSchemaUnsupportedType,
			expected: "schema type is not supported",
		},
		{
			name:     "ErrSchemaNotObject",
			err:      ErrSchemaNotObject,
			expected: "schema type must be object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestHasAdditionalProperties(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		schema   string
		expected bool
	}{
		{
			name: "additionalProperties true",
			schema: `{
				"type": "object",
				"additionalProperties": true
			}`,
			expected: true,
		},
		{
			name: "additionalProperties false",
			schema: `{
				"type": "object",
				"additionalProperties": false
			}`,
			expected: false,
		},
		{
			name: "additionalProperties with schema",
			schema: `{
				"type": "object",
				"additionalProperties": {"type": "string"}
			}`,
			expected: true,
		},
		{
			name: "no additionalProperties",
			schema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiled, err := CompileSchema([]byte(tt.schema))
			require.NoError(t, err)
			require.Equal(t, tt.expected, hasAdditionalProperties(compiled))
		})
	}
}
