package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type APIKey struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewAPIKey(id uuid.UUID) APIKey {
	a := APIKey{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseAPIKey(value string) (APIKey, error) {
	if value == "" {
		return APIKey{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return APIKey{}, fmt.Errorf("%w: expected two segments (apikey:<uuid>)", ErrInvalid)
	}

	if parts[0] != "apikey" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return APIKey{}, fmt.Errorf("%w: expected apikey urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return APIKey{}, fmt.Errorf("%w: invalid apikey uuid", ErrInvalid)
	}

	return NewAPIKey(id), nil
}

func (u APIKey) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u APIKey) String() string {
	return "apikey" + delimiter + u.ID.String()
}

func (u APIKey) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("apikey urn to json: %w", err)
	}

	return b, nil
}

func (u *APIKey) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read apikey urn string from json: %w", err)
	}

	parsed, err := ParseAPIKey(s)
	if err != nil {
		return fmt.Errorf("parse apikey urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *APIKey) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into APIKey", value)
	}

	parsed, err := ParseAPIKey(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u APIKey) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u APIKey) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal apikey urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *APIKey) UnmarshalText(text []byte) error {
	parsed, err := ParseAPIKey(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal apikey urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *APIKey) validate() error {
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
