package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const userSessionPrefix = "user_session"

type UserSession struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewUserSession(id uuid.UUID) UserSession {
	a := UserSession{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseUserSession(value string) (UserSession, error) {
	if value == "" {
		return UserSession{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return UserSession{}, fmt.Errorf("%w: expected two segments (%s:<uuid>)", ErrInvalid, userSessionPrefix)
	}

	if parts[0] != userSessionPrefix {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return UserSession{}, fmt.Errorf("%w: expected %s urn (got: %q)", ErrInvalid, userSessionPrefix, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return UserSession{}, fmt.Errorf("%w: invalid user_session uuid", ErrInvalid)
	}

	return NewUserSession(id), nil
}

func (u UserSession) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u UserSession) String() string {
	return userSessionPrefix + delimiter + u.ID.String()
}

func (u UserSession) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("user_session urn to json: %w", err)
	}

	return b, nil
}

func (u *UserSession) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read user_session urn string from json: %w", err)
	}

	parsed, err := ParseUserSession(s)
	if err != nil {
		return fmt.Errorf("parse user_session urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UserSession) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into UserSession", value)
	}

	parsed, err := ParseUserSession(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u UserSession) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u UserSession) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal user_session urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *UserSession) UnmarshalText(text []byte) error {
	parsed, err := ParseUserSession(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal user_session urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UserSession) validate() error {
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
