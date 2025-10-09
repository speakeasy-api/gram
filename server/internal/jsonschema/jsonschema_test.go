package jsonschema

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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

func TestIsValidJSONSchema_RealWorldOpenAPIScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		schema      []byte
		shouldError bool
		errorMsg    string
	}{
		{
			name: "valid nested object from OpenAPI operation",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"pathParameters": {
						"type": "object",
						"properties": {
							"userId": {"type": "string"}
						}
					},
					"queryParameters": {
						"type": "object",
						"properties": {
							"limit": {"type": "integer"},
							"offset": {"type": "integer"}
						}
					},
					"requestBody": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"email": {"type": "string", "format": "email"}
						},
						"required": ["email"]
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "valid enum from OpenAPI parameter",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"status": {
						"type": "string",
						"enum": ["pending", "active", "completed", "failed"]
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "enum with mixed types",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"status": {
						"type": "string",
						"enum": ["pending", 123, true]
					}
				}
			}`),
			shouldError: false, // JSON Schema allows mixed-type enums, validation happens at runtime
			errorMsg:    "",
		},
		{
			name: "valid oneOf from OpenAPI discriminator",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"payload": {
						"oneOf": [
							{
								"type": "object",
								"properties": {
									"type": {"const": "user"},
									"name": {"type": "string"}
								}
							},
							{
								"type": "object",
								"properties": {
									"type": {"const": "organization"},
									"orgName": {"type": "string"}
								}
							}
						]
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "valid anyOf from OpenAPI response",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"result": {
						"anyOf": [
							{"type": "string"},
							{"type": "integer"},
							{"type": "boolean"}
						]
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "deeply nested schema from complex OpenAPI spec",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"data": {
						"type": "object",
						"properties": {
							"user": {
								"type": "object",
								"properties": {
									"profile": {
										"type": "object",
										"properties": {
											"settings": {
												"type": "object",
												"properties": {
													"notifications": {
														"type": "object",
														"properties": {
															"email": {"type": "boolean"}
														}
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "array with items schema from OpenAPI",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"users": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"id": {"type": "string"},
								"email": {"type": "string"}
							},
							"required": ["id", "email"]
						}
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "pattern validation from OpenAPI",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"email": {
						"type": "string",
						"pattern": "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "invalid pattern - malformed regex",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"field": {
						"type": "string",
						"pattern": "[unclosed"
					}
				}
			}`),
			shouldError: true,
			errorMsg:    "compile json schema",
		},
		{
			name: "format validation from OpenAPI",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"createdAt": {"type": "string", "format": "date-time"},
					"date": {"type": "string", "format": "date"},
					"uuid": {"type": "string", "format": "uuid"},
					"uri": {"type": "string", "format": "uri"}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "allOf composition from OpenAPI inheritance",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"entity": {
						"allOf": [
							{
								"type": "object",
								"properties": {
									"id": {"type": "string"}
								}
							},
							{
								"type": "object",
								"properties": {
									"name": {"type": "string"}
								}
							}
						]
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "conflicting allOf schemas",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"field": {
						"allOf": [
							{"type": "string"},
							{"type": "integer"}
						]
					}
				}
			}`),
			shouldError: false, // Schema is valid, but validation against data would fail
			errorMsg:    "",
		},
		{
			name: "additionalProperties with schema",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"metadata": {
						"type": "object",
						"additionalProperties": {
							"type": "string"
						}
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "invalid - additionalProperties with wrong type",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"metadata": {
						"type": "object",
						"additionalProperties": "not-a-schema"
					}
				}
			}`),
			shouldError: true,
			errorMsg:    "compile json schema",
		},
		{
			name: "nullable field from OpenAPI nullable: true",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"optionalField": {
						"type": ["string", "null"]
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "min/max constraints from OpenAPI",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"age": {
						"type": "integer",
						"minimum": 0,
						"maximum": 150
					},
					"name": {
						"type": "string",
						"minLength": 1,
						"maxLength": 100
					},
					"tags": {
						"type": "array",
						"minItems": 1,
						"maxItems": 10
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "response filter schema with nested properties",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"responseFilter": {
						"type": "object",
						"properties": {
							"filter": {"type": "string"},
							"schema": {
								"type": "object",
								"properties": {
									"result": {"type": "string"}
								}
							}
						}
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
		{
			name: "multipart form data from OpenAPI",
			schema: []byte(`{
				"type": "object",
				"properties": {
					"file": {
						"type": "string",
						"format": "binary"
					},
					"metadata": {
						"type": "object",
						"properties": {
							"filename": {"type": "string"}
						}
					}
				}
			}`),
			shouldError: false,
			errorMsg:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := IsValidJSONSchema(tt.schema)
			if tt.shouldError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
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

func TestIsValidJSONSchema_RecursiveSchema(t *testing.T) {
	t.Parallel()

	schema := []byte(`{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"$defs": {
			"Filter": {
				"type": "object",
				"properties": {
					"operator": {
						"type": "string",
						"enum": ["AND", "OR", "NOT"]
					},
					"clauses": {
						"type": "array",
						"items": {
							"$ref": "#/$defs/Filter"
						}
					},
					"field": {
						"type": "string"
					},
					"value": {}
				}
			}
		},
		"$ref": "#/$defs/Filter"
	}`)

	err := IsValidJSONSchema(schema)
	require.NoError(t, err)
}

func TestIsValidJSONSchema_RecursiveSchema2(t *testing.T) {
	t.Parallel()

	schema := []byte(`{
  "type": "object",
  "properties": {
    "pathParameters": {
      "type": "object",
      "properties": {
        "id": {
          "type": "string",
          "title": "Id",
          "format": "uuid4",
          "description": "The meter ID."
        }
      },
      "required": [
        "id"
      ],
      "additionalProperties": false
    },
    "body": {
      "type": "object",
      "properties": {
        "metadata": {
          "type": "object",
          "propertyNames": {
            "maxLength": 40,
            "minLength": 1
          },
          "title": "Metadata",
          "maxProperties": 50,
          "additionalProperties": {
            "anyOf": [
              {
                "type": "string",
                "maxLength": 500,
                "minLength": 1
              },
              {
                "type": "integer"
              },
              {
                "type": "number"
              },
              {
                "type": "boolean"
              }
            ]
          },
          "description": "Key-value object allowing you to store additional information.\n\nThe key must be a string with a maximum length of **40 characters**.\nThe value must be either:\n\n* A string with a maximum length of **500 characters**\n* An integer\n* A floating-point number\n* A boolean\n\nYou can store up to **50 key-value pairs**."
        },
        "name": {
          "anyOf": [
            {
              "type": "string",
              "minLength": 3
            },
            {
              "type": "null"
            }
          ],
          "title": "Name",
          "description": "The name of the meter. Will be shown on customer's invoices and usage."
        },
        "filter": {
          "anyOf": [
            {
              "$ref": "#/$defs/Filter"
            },
            {
              "type": "null"
            }
          ],
          "description": "The filter to apply on events that'll be used to calculate the meter."
        },
        "aggregation": {
          "anyOf": [
            {
              "oneOf": [
                {
                  "type": "object",
                  "properties": {
                    "func": {
                      "type": "string",
                      "title": "Func",
                      "default": "count",
                      "const": "count"
                    }
                  },
                  "title": "CountAggregation"
                },
                {
                  "type": "object",
                  "properties": {
                    "func": {
                      "type": "string",
                      "title": "Func",
                      "enum": [
                        "sum",
                        "max",
                        "min",
                        "avg"
                      ]
                    },
                    "property": {
                      "type": "string",
                      "title": "Property"
                    }
                  },
                  "title": "PropertyAggregation",
                  "required": [
                    "func",
                    "property"
                  ]
                },
                {
                  "type": "object",
                  "properties": {
                    "func": {
                      "type": "string",
                      "title": "Func",
                      "default": "unique",
                      "const": "unique"
                    },
                    "property": {
                      "type": "string",
                      "title": "Property"
                    }
                  },
                  "title": "UniqueAggregation",
                  "required": [
                    "property"
                  ]
                }
              ],
              "discriminator": {
                "propertyName": "func",
                "mapping": {
                  "avg": "#/components/schemas/PropertyAggregation",
                  "count": "#/components/schemas/CountAggregation",
                  "max": "#/components/schemas/PropertyAggregation",
                  "min": "#/components/schemas/PropertyAggregation",
                  "sum": "#/components/schemas/PropertyAggregation",
                  "unique": "#/components/schemas/UniqueAggregation"
                }
              }
            },
            {
              "type": "null"
            }
          ],
          "title": "Aggregation",
          "description": "The aggregation to apply on the filtered events to calculate the meter."
        },
        "is_archived": {
          "anyOf": [
            {
              "type": "boolean"
            },
            {
              "type": "null"
            }
          ],
          "title": "Is Archived",
          "description": "Whether the meter is archived. Archived meters are no longer used for billing."
        }
      },
      "title": "MeterUpdate"
    }
  },
  "required": [
    "pathParameters",
    "body"
  ],
  "additionalProperties": false
}`)

	err := IsValidJSONSchema(schema)
	require.NoError(t, err)
}

func TestIsValidJSONSchema_FromOpenAPIFixture(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		fixtureFile    string
		invalidSchemas []string
	}{
		{
			name:           "valid fixture",
			fixtureFile:    "../deployments/fixtures/todo-valid.yaml",
			invalidSchemas: []string{},
		},
		{
			name:           "invalid fixture",
			fixtureFile:    "../deployments/fixtures/todo-invalid.yaml",
			invalidSchemas: []string{"Todo", "CreateTodoRequest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Read the OpenAPI fixture
			data, err := os.ReadFile(tt.fixtureFile)
			require.NoError(t, err)

			// Parse the YAML to extract schemas from components/schemas
			var doc struct {
				Components struct {
					Schemas map[string]interface{} `yaml:"schemas"`
				} `yaml:"components"`
			}

			err = yaml.Unmarshal(data, &doc)
			require.NoError(t, err)

			// Validate each schema in components/schemas
			require.NotEmpty(t, doc.Components.Schemas, "expected to find schemas in components/schemas")

			for schemaName, schemaObj := range doc.Components.Schemas {
				t.Run("schema_"+schemaName, func(t *testing.T) {
					t.Parallel()

					// Convert the schema object to JSON bytes
					schemaBytes, err := json.Marshal(schemaObj)
					require.NoError(t, err)

					// Validate the JSON schema
					err = IsValidJSONSchema(schemaBytes)

					// Check if this schema is expected to be invalid
					shouldBeInvalid := false
					for _, invalid := range tt.invalidSchemas {
						if invalid == schemaName {
							shouldBeInvalid = true
							break
						}
					}

					if shouldBeInvalid {
						require.Error(t, err, "schema %s should be invalid", schemaName)
						require.Contains(t, err.Error(), "compile json schema")
					} else {
						require.NoError(t, err, "schema %s should be valid", schemaName)
					}
				})
			}
		})
	}
}
