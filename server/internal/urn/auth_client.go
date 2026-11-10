package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type AuthClient struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewAuthClient(id uuid.UUID) AuthClient {
	a := AuthClient{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseAuthClient(value string) (AuthClient, error) {
	if value == "" {
		return AuthClient{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return AuthClient{}, fmt.Errorf("%w: expected two segments (auth_client:<uuid>)", ErrInvalid)
	}

	if parts[0] != "auth_client" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return AuthClient{}, fmt.Errorf("%w: expected auth_client urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return AuthClient{}, fmt.Errorf("%w: invalid auth_client uuid", ErrInvalid)
	}

	a := NewAuthClient(id)
	if a.err != nil {
		return AuthClient{}, a.err
	}

	return a, nil
}

func (u AuthClient) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u AuthClient) String() string {
	return "auth_client" + delimiter + u.ID.String()
}

func (u AuthClient) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("auth_client urn to json: %w", err)
	}

	return b, nil
}

func (u *AuthClient) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read auth_client urn string from json: %w", err)
	}

	parsed, err := ParseAuthClient(s)
	if err != nil {
		return fmt.Errorf("parse auth_client urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AuthClient) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into AuthClient", value)
	}

	parsed, err := ParseAuthClient(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u AuthClient) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u AuthClient) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal auth_client urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *AuthClient) UnmarshalText(text []byte) error {
	parsed, err := ParseAuthClient(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal auth_client urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AuthClient) validate() error {
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
