package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Toolset struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewToolset(id uuid.UUID) Toolset {
	t := Toolset{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = t.validate()

	return t
}

func ParseToolset(value string) (Toolset, error) {
	if value == "" {
		return Toolset{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return Toolset{}, fmt.Errorf("%w: expected two segments (toolset:<uuid>)", ErrInvalid)
	}

	if parts[0] != "toolset" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Toolset{}, fmt.Errorf("%w: expected toolset urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Toolset{}, fmt.Errorf("%w: invalid toolset uuid", ErrInvalid)
	}

	return NewToolset(id), nil
}

func (u Toolset) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u Toolset) String() string {
	return "toolset" + delimiter + u.ID.String()
}

func (u Toolset) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("toolset urn to json: %w", err)
	}

	return b, nil
}

func (u *Toolset) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read toolset urn string from json: %w", err)
	}

	parsed, err := ParseToolset(s)
	if err != nil {
		return fmt.Errorf("parse toolset urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Toolset) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Toolset", value)
	}

	parsed, err := ParseToolset(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Toolset) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Toolset) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal toolset urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Toolset) UnmarshalText(text []byte) error {
	parsed, err := ParseToolset(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal toolset urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Toolset) validate() error {
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
