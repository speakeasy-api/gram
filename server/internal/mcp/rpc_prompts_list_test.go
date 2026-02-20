package mcp

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePromptArgumentsFromJSONSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("parses_valid_schema_with_required_properties", func(t *testing.T) {
		t.Parallel()
		schema := `{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "The name of the user"
				},
				"age": {
					"type": "integer",
					"description": "The age of the user"
				}
			},
			"required": ["name"]
		}`

		args := parsePromptArgumentsFromJSONSchema(schema, logger, ctx)
		require.Len(t, args, 2)

		// Find name arg
		var nameArg, ageArg *promptArgument
		for i := range args {
			if args[i].Name == "name" {
				nameArg = &args[i]
			} else if args[i].Name == "age" {
				ageArg = &args[i]
			}
		}

		require.NotNil(t, nameArg)
		require.True(t, nameArg.Required)
		require.Equal(t, "The name of the user", nameArg.Description)

		require.NotNil(t, ageArg)
		require.False(t, ageArg.Required)
		require.Equal(t, "The age of the user", ageArg.Description)
	})

	t.Run("returns_empty_args_for_invalid_json", func(t *testing.T) {
		t.Parallel()
		schema := `{invalid json}`

		args := parsePromptArgumentsFromJSONSchema(schema, logger, ctx)
		require.Empty(t, args)
	})

	t.Run("returns_empty_args_for_empty_schema", func(t *testing.T) {
		t.Parallel()
		schema := `{}`

		args := parsePromptArgumentsFromJSONSchema(schema, logger, ctx)
		require.Empty(t, args)
	})

	t.Run("handles_schema_with_no_required_properties", func(t *testing.T) {
		t.Parallel()
		schema := `{
			"type": "object",
			"properties": {
				"optional_field": {
					"type": "string",
					"description": "An optional field"
				}
			}
		}`

		args := parsePromptArgumentsFromJSONSchema(schema, logger, ctx)
		require.Len(t, args, 1)
		require.Equal(t, "optional_field", args[0].Name)
		require.False(t, args[0].Required)
	})

	t.Run("handles_schema_with_no_description", func(t *testing.T) {
		t.Parallel()
		schema := `{
			"type": "object",
			"properties": {
				"field_no_desc": {
					"type": "string"
				}
			}
		}`

		args := parsePromptArgumentsFromJSONSchema(schema, logger, ctx)
		require.Len(t, args, 1)
		require.Equal(t, "field_no_desc", args[0].Name)
		require.Empty(t, args[0].Description)
	})

	t.Run("handles_multiple_required_properties", func(t *testing.T) {
		t.Parallel()
		schema := `{
			"type": "object",
			"properties": {
				"first": {"type": "string"},
				"second": {"type": "string"},
				"third": {"type": "string"}
			},
			"required": ["first", "third"]
		}`

		args := parsePromptArgumentsFromJSONSchema(schema, logger, ctx)
		require.Len(t, args, 3)

		requiredCount := 0
		for _, arg := range args {
			if arg.Required {
				requiredCount++
				require.True(t, arg.Name == "first" || arg.Name == "third")
			}
		}
		require.Equal(t, 2, requiredCount)
	})
}
