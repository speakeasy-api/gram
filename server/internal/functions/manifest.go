package functions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

type ManifestV0 struct {
	Version string           `json:"version"`
	Tools   []ManifestToolV0 `json:"tools"`
}

type ManifestToolV0 struct {
	Name        string                                  `json:"name"`
	Description string                                  `json:"description"`
	InputSchema json.RawMessage                         `json:"inputSchema"`
	Variables   map[string]*ManifestVariableAttributeV0 `json:"variables"`
}

type ManifestVariableAttributeV0 struct {
	Description *string `json:"description"`
}

type Manifest struct {
	Version string
	V0      *ManifestV0
}

func (m *Manifest) UnmarshalJSON(data []byte) error {
	var base struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &base); err != nil {
		return fmt.Errorf("unmarshal manifest version: %w", err)
	}

	m.Version = base.Version

	switch base.Version {
	case "0.0.0":
		var v0 ManifestV0
		if err := json.Unmarshal(data, &v0); err != nil {
			return fmt.Errorf("unmarshal manifest v0: %w", err)
		}
		m.V0 = &v0
	default:
		return fmt.Errorf("unknown manifest version: %s", base.Version)
	}

	return nil
}

func validateManifestToolV0(tool ManifestToolV0) (err error) {
	if tool.Name == "" {
		err = errors.Join(err, errors.New("tool name is required"))
	} else if !constants.SlugPatternRE.MatchString(tool.Name) {
		err = errors.Join(err, fmt.Errorf("tool name does not match regular expression: %s", constants.SlugPattern))
	}

	if tool.Description == "" {
		err = errors.Join(err, errors.New("tool description is required"))
	}
	if len(tool.InputSchema) > 0 {
		if jerr := isValidJSONSchema(tool.InputSchema); jerr != nil {
			err = errors.Join(err, fmt.Errorf("invalid tool input schema: %w", jerr))
		}
	}

	return
}

func isValidJSONSchema(bs []byte) error {
	compiler := jsonschema.NewCompiler()
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(bs))
	if err != nil {
		return fmt.Errorf("parse json schema bytes: %w", err)
	}
	if err := compiler.AddResource("file:///schema.json", rawSchema); err != nil {
		return fmt.Errorf("add json schema resource: %w", err)
	}
	if _, err := compiler.Compile("file:///schema.json"); err != nil {
		return fmt.Errorf("compile json schema: %w", err)
	}
	return nil
}
