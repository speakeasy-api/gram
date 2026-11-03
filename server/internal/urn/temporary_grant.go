package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type TemporaryGrant struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewTemporaryGrant(id uuid.UUID) TemporaryGrant {
	a := TemporaryGrant{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseTemporaryGrant(value string) (TemporaryGrant, error) {
	if value == "" {
		return TemporaryGrant{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return TemporaryGrant{}, fmt.Errorf("%w: expected two segments (temporary_grant:<uuid>)", ErrInvalid)
	}

	if parts[0] != "temporary_grant" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return TemporaryGrant{}, fmt.Errorf("%w: expected temporary_grant urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return TemporaryGrant{}, fmt.Errorf("%w: invalid temporary_grant uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return TemporaryGrant{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewTemporaryGrant(id), nil
}

func (u TemporaryGrant) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u TemporaryGrant) String() string {
	return "temporary_grant" + delimiter + u.ID.String()
}

func (u TemporaryGrant) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("temporary_grant urn to json: %w", err)
	}

	return b, nil
}

func (u *TemporaryGrant) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read temporary_grant urn string from json: %w", err)
	}

	parsed, err := ParseTemporaryGrant(s)
	if err != nil {
		return fmt.Errorf("parse temporary_grant urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TemporaryGrant) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into TemporaryGrant", value)
	}

	parsed, err := ParseTemporaryGrant(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u TemporaryGrant) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u TemporaryGrant) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal temporary_grant urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *TemporaryGrant) UnmarshalText(text []byte) error {
	parsed, err := ParseTemporaryGrant(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal temporary_grant urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TemporaryGrant) validate() error {
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
