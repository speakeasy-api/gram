package openapi

import "encoding/json"

// We will store shared openapi types in this package

type OpenapiV3ParameterProxy struct {
	Schema          json.RawMessage `json:"schema,omitempty" yaml:"schema,omitempty"`
	In              string          `json:"in,omitempty" yaml:"in,omitempty"`
	Name            string          `json:"name,omitempty" yaml:"name,omitempty"`
	Description     string          `json:"description,omitempty" yaml:"description,omitempty"`
	Required        *bool           `json:"required,omitempty" yaml:"required,omitempty"`
	Deprecated      bool            `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	AllowEmptyValue bool            `json:"allowEmptyValue,omitempty" yaml:"allowEmptyValue,omitempty"`
	Style           string          `json:"style,omitempty" yaml:"style,omitempty"`
	Explode         *bool           `json:"explode,omitempty" yaml:"explode,omitempty"`
}
