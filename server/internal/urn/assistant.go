package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Assistant struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewAssistant(id uuid.UUID) Assistant {
	a := Assistant{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseAssistant(value string) (Assistant, error) {
	if value == "" {
		return Assistant{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return Assistant{}, fmt.Errorf("%w: expected two segments (assistant:<uuid>)", ErrInvalid)
	}

	if parts[0] != "assistant" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Assistant{}, fmt.Errorf("%w: expected assistant urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Assistant{}, fmt.Errorf("%w: invalid assistant uuid", ErrInvalid)
	}

	return NewAssistant(id), nil
}

func (u Assistant) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u Assistant) String() string {
	return "assistant" + delimiter + u.ID.String()
}

func (u Assistant) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("assistant urn to json: %w", err)
	}

	return b, nil
}

func (u *Assistant) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read assistant urn string from json: %w", err)
	}

	parsed, err := ParseAssistant(s)
	if err != nil {
		return fmt.Errorf("parse assistant urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Assistant) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Assistant", value)
	}

	parsed, err := ParseAssistant(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Assistant) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Assistant) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal assistant urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Assistant) UnmarshalText(text []byte) error {
	parsed, err := ParseAssistant(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal assistant urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Assistant) validate() error {
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
