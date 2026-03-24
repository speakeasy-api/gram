package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Environment struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewEnvironment(id uuid.UUID) Environment {
	e := Environment{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = e.validate()

	return e
}

func ParseEnvironment(value string) (Environment, error) {
	if value == "" {
		return Environment{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return Environment{}, fmt.Errorf("%w: expected two segments (environment:<uuid>)", ErrInvalid)
	}

	if parts[0] != "environment" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Environment{}, fmt.Errorf("%w: expected environment urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Environment{}, fmt.Errorf("%w: invalid environment uuid", ErrInvalid)
	}

	return NewEnvironment(id), nil
}

func (u Environment) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u Environment) String() string {
	return "environment" + delimiter + u.ID.String()
}

func (u Environment) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("environment urn to json: %w", err)
	}

	return b, nil
}

func (u *Environment) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read environment urn string from json: %w", err)
	}

	parsed, err := ParseEnvironment(s)
	if err != nil {
		return fmt.Errorf("parse environment urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Environment) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Environment", value)
	}

	parsed, err := ParseEnvironment(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Environment) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Environment) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal environment urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Environment) UnmarshalText(text []byte) error {
	parsed, err := ParseEnvironment(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal environment urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Environment) validate() error {
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
