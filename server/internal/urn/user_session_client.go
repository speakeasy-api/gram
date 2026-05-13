package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const userSessionClientPrefix = "user_session_client"

type UserSessionClient struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewUserSessionClient(id uuid.UUID) UserSessionClient {
	a := UserSessionClient{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseUserSessionClient(value string) (UserSessionClient, error) {
	if value == "" {
		return UserSessionClient{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return UserSessionClient{}, fmt.Errorf("%w: expected two segments (%s:<uuid>)", ErrInvalid, userSessionClientPrefix)
	}

	if parts[0] != userSessionClientPrefix {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return UserSessionClient{}, fmt.Errorf("%w: expected %s urn (got: %q)", ErrInvalid, userSessionClientPrefix, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return UserSessionClient{}, fmt.Errorf("%w: invalid user_session_client uuid", ErrInvalid)
	}

	return NewUserSessionClient(id), nil
}

func (u UserSessionClient) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u UserSessionClient) String() string {
	return userSessionClientPrefix + delimiter + u.ID.String()
}

func (u UserSessionClient) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("user_session_client urn to json: %w", err)
	}

	return b, nil
}

func (u *UserSessionClient) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read user_session_client urn string from json: %w", err)
	}

	parsed, err := ParseUserSessionClient(s)
	if err != nil {
		return fmt.Errorf("parse user_session_client urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UserSessionClient) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into UserSessionClient", value)
	}

	parsed, err := ParseUserSessionClient(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u UserSessionClient) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u UserSessionClient) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal user_session_client urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *UserSessionClient) UnmarshalText(text []byte) error {
	parsed, err := ParseUserSessionClient(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal user_session_client urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UserSessionClient) validate() error {
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
