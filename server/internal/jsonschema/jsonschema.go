package jsonschema

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

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

// newCompiler creates a jsonschema compiler with lenient regex handling. It
// allows PCRE patterns that are incompatible with Go's RE2 engine to pass
// through, while still rejecting genuinely invalid regex patterns.
func newCompiler() *jsonschema.Compiler {
	compiler := jsonschema.NewCompiler()

	compiler.UseRegexpEngine(func(pattern string) (jsonschema.Regexp, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			if isPCREOnlyError(err) {
				return &noopRegexp{pattern: pattern}, nil
			}
			return nil, fmt.Errorf("invalid regex '%s': %w", pattern, err)
		}
		return &regexpAdapter{Regexp: re}, nil
	})

	return compiler
}

// isPCREOnlyError checks if a regexp compilation error is due to PCRE-specific
// features that are not supported by Go's RE2 engine, rather than genuinely
// invalid syntax.
func isPCREOnlyError(err error) bool {
	errStr := err.Error()
	pcreFeatures := []string{
		"invalid named capture: `(?<!",
		"invalid named capture: `(?<=",
		"invalid or unsupported Perl syntax",
	}

	for _, feature := range pcreFeatures {
		if strings.Contains(errStr, feature) {
			return true
		}
	}

	return false
}

// noopRegexp is a regex matcher that always returns true, used for PCRE patterns
// that cannot be compiled with RE2. This allows schemas with PCRE patterns to pass
// validation without breaking on unsupported features.
type noopRegexp struct {
	pattern string
}

func (n *noopRegexp) MatchString(s string) bool {
	// Always match - skips pattern validation for PCRE-only features
	return true
}

func (n *noopRegexp) String() string {
	return n.pattern
}

type regexpAdapter struct {
	*regexp.Regexp
}

func (r *regexpAdapter) MatchString(s string) bool {
	return r.Regexp.MatchString(s)
}

func (r *regexpAdapter) String() string {
	return r.Regexp.String()
}

// IsValidJSONSchema validates that the provided bytes represent a valid JSON
// schema.
func IsValidJSONSchema(bs []byte) error {
	compiler := newCompiler()
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(bs))
	if err != nil {
		return fmt.Errorf("parse json schema bytes: %w", err)
	}
	if err = compiler.AddResource(schemaResourceURI, rawSchema); err != nil {
		return fmt.Errorf("add json schema resource: %w", err)
	}
	if _, err = compiler.Compile(schemaResourceURI); err != nil {
		return fmt.Errorf("compile json schema: %w", err)
	}
	return nil
}

// CompileSchema compiles a JSON schema from bytes and returns the compiled
// schema.
func CompileSchema(schemaBytes []byte) (*jsonschema.Schema, error) {
	compiler := newCompiler()
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("parse json schema: %w", err)
	}
	if err = compiler.AddResource(schemaResourceURI, rawSchema); err != nil {
		return nil, fmt.Errorf("add schema resource: %w", err)
	}
	schema, err := compiler.Compile(schemaResourceURI)
	if err != nil {
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

// ValidateInputSchema validates that an input schema is a valid object-type
// schema with properties or additionalProperties defined. This is specifically
// for validating schemas used as tool/template input definitions.
func ValidateInputSchema(rawInput io.Reader) error {
	rawSchema, err := jsonschema.UnmarshalJSON(rawInput)
	if err != nil {
		return fmt.Errorf("unmarshal schema: %w", err)
	}

	compiler := newCompiler()
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
