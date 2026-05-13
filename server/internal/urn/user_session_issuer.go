package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const userSessionIssuerPrefix = "user_session_issuer"

type UserSessionIssuer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewUserSessionIssuer(id uuid.UUID) UserSessionIssuer {
	a := UserSessionIssuer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseUserSessionIssuer(value string) (UserSessionIssuer, error) {
	if value == "" {
		return UserSessionIssuer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return UserSessionIssuer{}, fmt.Errorf("%w: expected two segments (%s:<uuid>)", ErrInvalid, userSessionIssuerPrefix)
	}

	if parts[0] != userSessionIssuerPrefix {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return UserSessionIssuer{}, fmt.Errorf("%w: expected %s urn (got: %q)", ErrInvalid, userSessionIssuerPrefix, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return UserSessionIssuer{}, fmt.Errorf("%w: invalid user_session_issuer uuid", ErrInvalid)
	}

	return NewUserSessionIssuer(id), nil
}

func (u UserSessionIssuer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u UserSessionIssuer) String() string {
	return userSessionIssuerPrefix + delimiter + u.ID.String()
}

func (u UserSessionIssuer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("user_session_issuer urn to json: %w", err)
	}

	return b, nil
}

func (u *UserSessionIssuer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read user_session_issuer urn string from json: %w", err)
	}

	parsed, err := ParseUserSessionIssuer(s)
	if err != nil {
		return fmt.Errorf("parse user_session_issuer urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UserSessionIssuer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into UserSessionIssuer", value)
	}

	parsed, err := ParseUserSessionIssuer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u UserSessionIssuer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u UserSessionIssuer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal user_session_issuer urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *UserSessionIssuer) UnmarshalText(text []byte) error {
	parsed, err := ParseUserSessionIssuer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal user_session_issuer urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UserSessionIssuer) validate() error {
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
