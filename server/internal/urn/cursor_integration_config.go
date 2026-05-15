package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type CursorIntegrationConfig struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewCursorIntegrationConfig(id uuid.UUID) CursorIntegrationConfig {
	c := CursorIntegrationConfig{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = c.validate()

	return c
}

func ParseCursorIntegrationConfig(value string) (CursorIntegrationConfig, error) {
	if value == "" {
		return CursorIntegrationConfig{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return CursorIntegrationConfig{}, fmt.Errorf("%w: expected two segments (cursor_integration_config:<uuid>)", ErrInvalid)
	}

	if parts[0] != "cursor_integration_config" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return CursorIntegrationConfig{}, fmt.Errorf("%w: expected cursor_integration_config urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return CursorIntegrationConfig{}, fmt.Errorf("%w: invalid cursor_integration_config uuid", ErrInvalid)
	}

	return NewCursorIntegrationConfig(id), nil
}

func (u CursorIntegrationConfig) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u CursorIntegrationConfig) String() string {
	return "cursor_integration_config" + delimiter + u.ID.String()
}

func (u CursorIntegrationConfig) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("cursor_integration_config urn to json: %w", err)
	}

	return b, nil
}

func (u *CursorIntegrationConfig) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read cursor_integration_config urn string from json: %w", err)
	}

	parsed, err := ParseCursorIntegrationConfig(s)
	if err != nil {
		return fmt.Errorf("parse cursor_integration_config urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *CursorIntegrationConfig) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into CursorIntegrationConfig", value)
	}

	parsed, err := ParseCursorIntegrationConfig(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u CursorIntegrationConfig) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u CursorIntegrationConfig) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal cursor_integration_config urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *CursorIntegrationConfig) UnmarshalText(text []byte) error {
	parsed, err := ParseCursorIntegrationConfig(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal cursor_integration_config urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *CursorIntegrationConfig) validate() error {
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
