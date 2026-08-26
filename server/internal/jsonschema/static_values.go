package jsonschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// StaticValue is a literal carried by a JSON Schema keyword.
type StaticValue struct {
	// SchemaPath is a JSON Pointer to the schema object containing Keyword.
	SchemaPath string

	// Keyword is one of const, default, enum, example, or examples.
	Keyword string

	// Value is the decoded JSON value. Numbers remain json.Number values so
	// callers can render them without losing precision.
	Value any
}

var staticValueKeywords = []string{"const", "default", "enum", "example", "examples"}

var schemaMapKeywords = []string{
	"$defs", "definitions", "dependentSchemas", "patternProperties", "properties",
}

var schemaValueKeywords = []string{
	"additionalItems", "additionalProperties", "contains", "contentSchema", "else",
	"if", "items", "not", "propertyNames", "then", "unevaluatedItems", "unevaluatedProperties",
}

var schemaArrayKeywords = []string{"allOf", "anyOf", "oneOf", "prefixItems"}

// StaticValues returns every const, default, enum, example, and examples value
// in schema. SchemaPath identifies the schema object containing each keyword.
func StaticValues(schema []byte) ([]StaticValue, error) {
	dec := json.NewDecoder(bytes.NewReader(schema))
	dec.UseNumber()

	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode schema: %w", err)
	}

	values := make([]StaticValue, 0)
	collectStaticValues(root, nil, &values)
	sort.Slice(values, func(i, j int) bool {
		if values[i].SchemaPath != values[j].SchemaPath {
			return values[i].SchemaPath < values[j].SchemaPath
		}
		return values[i].Keyword < values[j].Keyword
	})

	return values, nil
}

func collectStaticValues(node any, path []string, values *[]StaticValue) {
	schema, ok := node.(map[string]any)
	if !ok {
		return
	}

	for _, keyword := range staticValueKeywords {
		if value, ok := schema[keyword]; ok {
			*values = append(*values, StaticValue{
				SchemaPath: jsonPointer(path),
				Keyword:    keyword,
				Value:      value,
			})
		}
	}

	for _, keyword := range schemaMapKeywords {
		children, ok := schema[keyword].(map[string]any)
		if !ok {
			continue
		}
		for _, name := range sortedMapKeys(children) {
			collectStaticValues(children[name], appendPath(path, keyword, name), values)
		}
	}

	for _, keyword := range schemaValueKeywords {
		child := schema[keyword]
		if keyword == "items" {
			if children, ok := child.([]any); ok {
				for i, item := range children {
					collectStaticValues(item, appendPath(path, keyword, fmt.Sprintf("%d", i)), values)
				}
				continue
			}
		}
		collectStaticValues(child, appendPath(path, keyword), values)
	}

	for _, keyword := range schemaArrayKeywords {
		children, ok := schema[keyword].([]any)
		if !ok {
			continue
		}
		for i, child := range children {
			collectStaticValues(child, appendPath(path, keyword, fmt.Sprintf("%d", i)), values)
		}
	}

	// Draft 7 dependencies may contain either property-name arrays or schemas.
	if dependencies, ok := schema["dependencies"].(map[string]any); ok {
		for _, name := range sortedMapKeys(dependencies) {
			collectStaticValues(dependencies[name], appendPath(path, "dependencies", name), values)
		}
	}
}

func appendPath(path []string, tokens ...string) []string {
	return append(slices.Clone(path), tokens...)
}

func jsonPointer(path []string) string {
	if len(path) == 0 {
		return ""
	}

	escaped := make([]string, len(path))
	for i, token := range path {
		escaped[i] = strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
	}
	return "/" + strings.Join(escaped, "/")
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
