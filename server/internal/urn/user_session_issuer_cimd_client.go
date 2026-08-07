package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type UserSessionIssuerCimdClient struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewUserSessionIssuerCimdClient(id uuid.UUID) UserSessionIssuerCimdClient {
	a := UserSessionIssuerCimdClient{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseUserSessionIssuerCimdClient(value string) (UserSessionIssuerCimdClient, error) {
	if value == "" {
		return UserSessionIssuerCimdClient{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return UserSessionIssuerCimdClient{}, fmt.Errorf("%w: expected two segments (user-session-issuer-cimd-client:<uuid>)", ErrInvalid)
	}

	if parts[0] != "user-session-issuer-cimd-client" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return UserSessionIssuerCimdClient{}, fmt.Errorf("%w: expected user-session-issuer-cimd-client urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return UserSessionIssuerCimdClient{}, fmt.Errorf("%w: invalid user-session-issuer-cimd-client uuid", ErrInvalid)
	}

	return NewUserSessionIssuerCimdClient(id), nil
}

func (u UserSessionIssuerCimdClient) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u UserSessionIssuerCimdClient) String() string {
	return "user-session-issuer-cimd-client" + delimiter + u.ID.String()
}

func (u UserSessionIssuerCimdClient) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("user-session-issuer-cimd-client urn to json: %w", err)
	}

	return b, nil
}

func (u *UserSessionIssuerCimdClient) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read user-session-issuer-cimd-client urn string from json: %w", err)
	}

	parsed, err := ParseUserSessionIssuerCimdClient(s)
	if err != nil {
		return fmt.Errorf("parse user-session-issuer-cimd-client urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UserSessionIssuerCimdClient) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into UserSessionIssuerCimdClient", value)
	}

	parsed, err := ParseUserSessionIssuerCimdClient(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u UserSessionIssuerCimdClient) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u UserSessionIssuerCimdClient) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal user-session-issuer-cimd-client urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *UserSessionIssuerCimdClient) UnmarshalText(text []byte) error {
	parsed, err := ParseUserSessionIssuerCimdClient(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal user-session-issuer-cimd-client urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UserSessionIssuerCimdClient) validate() error {
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
