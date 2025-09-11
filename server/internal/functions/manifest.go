package functions

import (
	"encoding/json"
	"fmt"
)

type ManifestV0 struct {
	Version string           `json:"version"`
	Tools   []ManifestToolV0 `json:"tools"`
}

type ManifestToolV0 struct {
	Name        string                                  `json:"name"`
	Description string                                  `json:"description"`
	InputSchema json.RawMessage                         `json:"input_schema"`
	Variables   map[string]*ManifestVariableAttributeV0 `json:"variables"`
}

type ManifestVariableAttributeV0 struct {
	Description *string `json:"description"`
}

type Manifest struct {
	V0 *ManifestV0
}

func (m *Manifest) UnmarshalJSON(data []byte) error {
	var base struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &base); err != nil {
		return fmt.Errorf("unmarshal manifest version: %w", err)
	}

	switch base.Version {
	case "0":
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
