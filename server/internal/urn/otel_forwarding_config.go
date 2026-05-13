package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type OtelForwardingConfig struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewOtelForwardingConfig(id uuid.UUID) OtelForwardingConfig {
	c := OtelForwardingConfig{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = c.validate()

	return c
}

func ParseOtelForwardingConfig(value string) (OtelForwardingConfig, error) {
	if value == "" {
		return OtelForwardingConfig{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return OtelForwardingConfig{}, fmt.Errorf("%w: expected two segments (otel_forwarding_config:<uuid>)", ErrInvalid)
	}

	if parts[0] != "otel_forwarding_config" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return OtelForwardingConfig{}, fmt.Errorf("%w: expected otel_forwarding_config urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return OtelForwardingConfig{}, fmt.Errorf("%w: invalid otel_forwarding_config uuid", ErrInvalid)
	}

	return NewOtelForwardingConfig(id), nil
}

func (u OtelForwardingConfig) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u OtelForwardingConfig) String() string {
	return "otel_forwarding_config" + delimiter + u.ID.String()
}

func (u OtelForwardingConfig) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("otel_forwarding_config urn to json: %w", err)
	}

	return b, nil
}

func (u *OtelForwardingConfig) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read otel_forwarding_config urn string from json: %w", err)
	}

	parsed, err := ParseOtelForwardingConfig(s)
	if err != nil {
		return fmt.Errorf("parse otel_forwarding_config urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OtelForwardingConfig) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into OtelForwardingConfig", value)
	}

	parsed, err := ParseOtelForwardingConfig(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u OtelForwardingConfig) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u OtelForwardingConfig) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal otel_forwarding_config urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *OtelForwardingConfig) UnmarshalText(text []byte) error {
	parsed, err := ParseOtelForwardingConfig(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal otel_forwarding_config urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OtelForwardingConfig) validate() error {
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
