package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type OtelDestination struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewOtelDestination(id uuid.UUID) OtelDestination {
	c := OtelDestination{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = c.validate()

	return c
}

func ParseOtelDestination(value string) (OtelDestination, error) {
	if value == "" {
		return OtelDestination{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return OtelDestination{}, fmt.Errorf("%w: expected two segments (otel_destination:<uuid>)", ErrInvalid)
	}

	if parts[0] != "otel_destination" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return OtelDestination{}, fmt.Errorf("%w: expected otel_destination urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return OtelDestination{}, fmt.Errorf("%w: invalid otel_destination uuid", ErrInvalid)
	}

	return NewOtelDestination(id), nil
}

func (u OtelDestination) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u OtelDestination) String() string {
	return "otel_destination" + delimiter + u.ID.String()
}

func (u OtelDestination) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("otel_destination urn to json: %w", err)
	}

	return b, nil
}

func (u *OtelDestination) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read otel_destination urn string from json: %w", err)
	}

	parsed, err := ParseOtelDestination(s)
	if err != nil {
		return fmt.Errorf("parse otel_destination urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OtelDestination) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into OtelDestination", value)
	}

	parsed, err := ParseOtelDestination(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u OtelDestination) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u OtelDestination) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal otel_destination urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *OtelDestination) UnmarshalText(text []byte) error {
	parsed, err := ParseOtelDestination(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal otel_destination urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OtelDestination) validate() error {
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
