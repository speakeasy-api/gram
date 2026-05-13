package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const userSessionConsentPrefix = "user_session_consent"

type UserSessionConsent struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewUserSessionConsent(id uuid.UUID) UserSessionConsent {
	a := UserSessionConsent{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseUserSessionConsent(value string) (UserSessionConsent, error) {
	if value == "" {
		return UserSessionConsent{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return UserSessionConsent{}, fmt.Errorf("%w: expected two segments (%s:<uuid>)", ErrInvalid, userSessionConsentPrefix)
	}

	if parts[0] != userSessionConsentPrefix {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return UserSessionConsent{}, fmt.Errorf("%w: expected %s urn (got: %q)", ErrInvalid, userSessionConsentPrefix, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return UserSessionConsent{}, fmt.Errorf("%w: invalid user_session_consent uuid", ErrInvalid)
	}

	return NewUserSessionConsent(id), nil
}

func (u UserSessionConsent) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u UserSessionConsent) String() string {
	return userSessionConsentPrefix + delimiter + u.ID.String()
}

func (u UserSessionConsent) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("user_session_consent urn to json: %w", err)
	}

	return b, nil
}

func (u *UserSessionConsent) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read user_session_consent urn string from json: %w", err)
	}

	parsed, err := ParseUserSessionConsent(s)
	if err != nil {
		return fmt.Errorf("parse user_session_consent urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UserSessionConsent) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into UserSessionConsent", value)
	}

	parsed, err := ParseUserSessionConsent(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u UserSessionConsent) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u UserSessionConsent) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal user_session_consent urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *UserSessionConsent) UnmarshalText(text []byte) error {
	parsed, err := ParseUserSessionConsent(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal user_session_consent urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UserSessionConsent) validate() error {
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
