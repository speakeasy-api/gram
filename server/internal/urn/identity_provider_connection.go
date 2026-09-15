package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const identityProviderConnectionPrefix = "identityproviderconnection"

type IdentityProviderConnectionID struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewIdentityProviderConnectionID(id uuid.UUID) IdentityProviderConnectionID {
	value := IdentityProviderConnectionID{
		ID:      id,
		checked: false,
		err:     nil,
	}
	_ = value.validate()
	return value
}

func ParseIdentityProviderConnectionID(value string) (IdentityProviderConnectionID, error) {
	if value == "" {
		return IdentityProviderConnectionID{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return IdentityProviderConnectionID{}, fmt.Errorf("%w: expected two segments (%s:<uuid>)", ErrInvalid, identityProviderConnectionPrefix)
	}
	if parts[0] != identityProviderConnectionPrefix {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return IdentityProviderConnectionID{}, fmt.Errorf("%w: expected identity provider connection urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return IdentityProviderConnectionID{}, fmt.Errorf("%w: invalid identity provider connection uuid", ErrInvalid)
	}

	return NewIdentityProviderConnectionID(id), nil
}

func (u IdentityProviderConnectionID) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u IdentityProviderConnectionID) String() string {
	return identityProviderConnectionPrefix + delimiter + u.ID.String()
}

func (u IdentityProviderConnectionID) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("identity provider connection urn to json: %w", err)
	}
	return b, nil
}

func (u *IdentityProviderConnectionID) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("read identity provider connection urn string from json: %w", err)
	}

	parsed, err := ParseIdentityProviderConnectionID(value)
	if err != nil {
		return fmt.Errorf("parse identity provider connection urn json string: %w", err)
	}
	*u = parsed
	return nil
}

func (u *IdentityProviderConnectionID) Scan(value any) error {
	if value == nil {
		return nil
	}

	var raw string
	switch typed := value.(type) {
	case string:
		raw = typed
	case []byte:
		raw = string(typed)
	default:
		return fmt.Errorf("cannot scan %T into IdentityProviderConnectionID", value)
	}

	parsed, err := ParseIdentityProviderConnectionID(raw)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}
	*u = parsed
	return nil
}

func (u IdentityProviderConnectionID) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	return u.String(), nil
}

func (u IdentityProviderConnectionID) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal identity provider connection urn text: %w", err)
	}
	return []byte(u.String()), nil
}

func (u *IdentityProviderConnectionID) UnmarshalText(text []byte) error {
	parsed, err := ParseIdentityProviderConnectionID(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal identity provider connection urn text: %w", err)
	}
	*u = parsed
	return nil
}

func (u *IdentityProviderConnectionID) validate() error {
	if u.checked {
		return u.err
	}
	u.checked = true
	if u.ID == uuid.Nil {
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
	}
	return u.err
}
