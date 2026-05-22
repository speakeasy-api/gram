package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type AIIntegrationConfig struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewAIIntegrationConfig(id uuid.UUID) AIIntegrationConfig {
	c := AIIntegrationConfig{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = c.validate()

	return c
}

func ParseAIIntegrationConfig(value string) (AIIntegrationConfig, error) {
	if value == "" {
		return AIIntegrationConfig{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return AIIntegrationConfig{}, fmt.Errorf("%w: expected two segments (ai_integration_config:<uuid>)", ErrInvalid)
	}

	if parts[0] != "ai_integration_config" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return AIIntegrationConfig{}, fmt.Errorf("%w: expected ai_integration_config urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return AIIntegrationConfig{}, fmt.Errorf("%w: invalid ai_integration_config uuid", ErrInvalid)
	}

	return NewAIIntegrationConfig(id), nil
}

func (u AIIntegrationConfig) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u AIIntegrationConfig) String() string {
	return "ai_integration_config" + delimiter + u.ID.String()
}

func (u AIIntegrationConfig) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("ai_integration_config urn to json: %w", err)
	}

	return b, nil
}

func (u *AIIntegrationConfig) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read ai_integration_config urn string from json: %w", err)
	}

	parsed, err := ParseAIIntegrationConfig(s)
	if err != nil {
		return fmt.Errorf("parse ai_integration_config urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AIIntegrationConfig) Scan(value any) error {
	if value == nil {
		return nil
	}

	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("cannot scan %T into AIIntegrationConfig", value)
	}

	parsed, err := ParseAIIntegrationConfig(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u AIIntegrationConfig) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u AIIntegrationConfig) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal ai_integration_config urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *AIIntegrationConfig) UnmarshalText(text []byte) error {
	parsed, err := ParseAIIntegrationConfig(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal ai_integration_config urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AIIntegrationConfig) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	if u.ID == uuid.Nil {
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
		return u.err
	}

	return nil
}
