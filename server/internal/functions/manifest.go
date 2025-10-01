package functions

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/jsonschema"
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
		if jerr := jsonschema.IsValidJSONSchema(tool.InputSchema); jerr != nil {
			err = errors.Join(err, fmt.Errorf("invalid tool input schema: %w", jerr))
		}
	}

	return
}
