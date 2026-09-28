package mcpregistry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry/contract"
)

const RecordInputByteLimit = 8 << 20

// StoredRecordByteLimit bounds PostgreSQL jsonb::text, not compressed storage.
const StoredRecordByteLimit = 8 << 20

type Issue struct {
	Path    string
	Message string
}

type InvalidError struct{ Issues []Issue }

func (e *InvalidError) Error() string { return "invalid registry record" }

type Validator struct {
	schema *jsonschema.Schema
}

func LoadValidator() (*Validator, error) {
	s, err := contract.Compile(contract.Schema)
	if err != nil {
		return nil, fmt.Errorf("load registry validator: %w", err)
	}
	return &Validator{schema: s}, nil
}

func (v *Validator) Validate(raw json.RawMessage) []Issue {
	return v.validate(raw, RecordInputByteLimit)
}

func (v *Validator) ValidateStored(raw json.RawMessage) []Issue {
	return v.validate(raw, StoredRecordByteLimit)
}

func (v *Validator) validate(raw json.RawMessage, byteLimit int) []Issue {
	if len(raw) > byteLimit {
		return []Issue{{Path: "", Message: "record exceeds byte limit"}}
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return []Issue{{Path: "", Message: "expected exactly one JSON value"}}
	}
	if err := v.schema.Validate(value); err != nil {
		return validationIssues(err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return []Issue{{Path: "", Message: "expected object"}}
	}
	server, _ := object["server"].(map[string]any)
	name, _ := server["name"].(string)
	if name == "" {
		return []Issue{{Path: "/server/name", Message: "nonempty name required"}}
	}
	return nil
}

// Never use ValidationError.Error: its text can include rejected values.
func validationIssues(err error) []Issue {
	var root *jsonschema.ValidationError
	if !errors.As(err, &root) {
		return []Issue{{Path: "", Message: "record does not conform to registry schema"}}
	}
	issues := make([]Issue, 0, 20)
	var visit func(*jsonschema.ValidationError)
	visit = func(e *jsonschema.ValidationError) {
		if len(issues) == 20 {
			return
		}
		if len(e.Causes) > 0 {
			for _, cause := range e.Causes {
				visit(cause)
				if len(issues) == 20 {
					return
				}
			}
			return
		}
		path := ""
		truncated := false
		for _, segment := range e.InstanceLocation {
			escaped := strings.ReplaceAll(strings.ReplaceAll(segment, "~", "~0"), "/", "~1")
			if len(path)+len(escaped)+1 > 256 {
				truncated = true
				break
			}
			path += "/" + escaped
		}
		message := "does not satisfy schema constraint"
		switch e.ErrorKind.(type) {
		case *kind.Type:
			message = "unexpected JSON type"
		case *kind.Required:
			message = "required property missing"
		case *kind.Format:
			message = "invalid format"
		case *kind.Pattern:
			message = "does not match required pattern"
		}
		if truncated {
			message += " (path truncated)"
		}
		issues = append(issues, Issue{Path: path, Message: message})
	}
	visit(root)
	return issues
}
