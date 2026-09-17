package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type IdentityProviderConnection struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewIdentityProviderConnection(id uuid.UUID) IdentityProviderConnection {
	c := IdentityProviderConnection{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = c.validate()

	return c
}

func ParseIdentityProviderConnection(value string) (IdentityProviderConnection, error) {
	if value == "" {
		return IdentityProviderConnection{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return IdentityProviderConnection{}, fmt.Errorf("%w: expected two segments (identity_provider_connection:<uuid>)", ErrInvalid)
	}

	if parts[0] != "identity_provider_connection" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return IdentityProviderConnection{}, fmt.Errorf("%w: expected identity_provider_connection urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return IdentityProviderConnection{}, fmt.Errorf("%w: invalid identity_provider_connection uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return IdentityProviderConnection{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewIdentityProviderConnection(id), nil
}

func (u IdentityProviderConnection) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u IdentityProviderConnection) String() string {
	return "identity_provider_connection" + delimiter + u.ID.String()
}

func (u IdentityProviderConnection) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("identity_provider_connection urn to json: %w", err)
	}

	return b, nil
}

func (u *IdentityProviderConnection) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read identity_provider_connection urn string from json: %w", err)
	}

	parsed, err := ParseIdentityProviderConnection(s)
	if err != nil {
		return fmt.Errorf("parse identity_provider_connection urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *IdentityProviderConnection) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into IdentityProviderConnection", value)
	}

	parsed, err := ParseIdentityProviderConnection(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u IdentityProviderConnection) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u IdentityProviderConnection) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal identity_provider_connection urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *IdentityProviderConnection) UnmarshalText(text []byte) error {
	parsed, err := ParseIdentityProviderConnection(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal identity_provider_connection urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *IdentityProviderConnection) validate() error {
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
