// Package contract owns the offline registry schema and its source-specific formats.
package contract

import (
	"bytes"
	_ "embed"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

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

// Compile rejects unknown formats instead of silently treating them as annotations.
func Compile(raw []byte) (*jsonschema.Schema, error) {
	value, err := decode(raw)
	if err != nil {
		return nil, err
	}
	formats := map[string]func(any) error{"gram-source-uri": validateURI, "gram-source-date-time": validateTimestamp}
	var check func(any) error
	check = func(v any) error {
		switch n := v.(type) {
		case map[string]any:
			if f, ok := n["format"].(string); ok && formats[f] == nil {
				return fmt.Errorf("unsupported contract format")
			}
			for keyword, child := range n {
				switch keyword {
				case "$defs", "definitions", "properties", "patternProperties", "dependentSchemas":
					if schemas, ok := child.(map[string]any); ok {
						for _, schema := range schemas {
							if err := check(schema); err != nil {
								return err
							}
						}
					}
				case "allOf", "anyOf", "oneOf", "prefixItems", "items", "contains", "additionalProperties", "propertyNames", "not", "if", "then", "else", "unevaluatedItems", "unevaluatedProperties":
					if err := check(child); err != nil {
						return err
					}
				}
			}
		case []any:
			for _, child := range n {
				if err := check(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := check(value); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	for name, validate := range formats {
		c.RegisterFormat(&jsonschema.Format{Name: name, Validate: validate})
	}
	if err := c.AddResource("record.schema.json", value); err != nil {
		return nil, fmt.Errorf("add registry contract resource: %w", err)
	}
	schema, err := c.Compile("record.schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile registry contract schema: %w", err)
	}
	return schema, nil
}

var literalPattern = regexp.MustCompile(`^[^:]+://(?:[^@/?#]*@)?(\[[^\]]+\])`)

func validateURI(value any) error {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	m := literalPattern.FindStringSubmatch(s)
	if m == nil || strings.HasPrefix(m[1], "[v") || strings.HasPrefix(m[1], "[V") {
		return nil
	}
	if !strings.Contains(m[1], ":") || net.ParseIP(strings.Trim(m[1], "[]")) == nil {
		return fmt.Errorf("invalid URI IP literal")
	}
	return nil
}

var timestampPattern = regexp.MustCompile(`^(\d{4})-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])[tT]([01]\d|2[0-3]):([0-5]\d):([0-5]\d|60)(?:\.\d+)?([zZ]|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)

func validateTimestamp(value any) error {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	p := timestampPattern.FindStringSubmatch(s)
	invalid := fmt.Errorf("invalid source RFC3339 timestamp")
	if p == nil {
		return invalid
	}
	integer := func(s string) int { n, _ := strconv.Atoi(s); return n }
	year, month, day := integer(p[1]), integer(p[2]), integer(p[3])
	days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
		days[1] = 29
	}
	if day > days[month-1] {
		return invalid
	}
	if p[6] != "60" {
		return nil
	}
	offset := 0
	if len(p[7]) != 1 {
		offset = integer(p[7][1:3])*60 + integer(p[7][4:])
		if p[7][0] == '-' {
			offset = -offset
		}
	}
	utcMinutes := integer(p[4])*60 + integer(p[5]) - offset
	if (utcMinutes == -1 && day == 1) || (utcMinutes == 1439 && day == days[month-1]) {
		return nil
	}
	return invalid
}
