package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type AccessGrant struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewAccessGrant(id uuid.UUID) AccessGrant {
	a := AccessGrant{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseAccessGrant(value string) (AccessGrant, error) {
	if value == "" {
		return AccessGrant{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return AccessGrant{}, fmt.Errorf("%w: expected two segments (access_grant:<uuid>)", ErrInvalid)
	}

	if parts[0] != "access_grant" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return AccessGrant{}, fmt.Errorf("%w: expected access_grant urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return AccessGrant{}, fmt.Errorf("%w: invalid access_grant uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return AccessGrant{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewAccessGrant(id), nil
}

func (u AccessGrant) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u AccessGrant) String() string {
	return "access_grant" + delimiter + u.ID.String()
}

func (u AccessGrant) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("access_grant urn to json: %w", err)
	}

	return b, nil
}

func (u *AccessGrant) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read access_grant urn string from json: %w", err)
	}

	parsed, err := ParseAccessGrant(s)
	if err != nil {
		return fmt.Errorf("parse access_grant urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AccessGrant) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into AccessGrant", value)
	}

	parsed, err := ParseAccessGrant(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u AccessGrant) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u AccessGrant) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal access_grant urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *AccessGrant) UnmarshalText(text []byte) error {
	parsed, err := ParseAccessGrant(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal access_grant urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AccessGrant) validate() error {
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
