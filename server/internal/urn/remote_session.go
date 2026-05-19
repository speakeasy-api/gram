package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type RemoteSession struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewRemoteSession(id uuid.UUID) RemoteSession {
	a := RemoteSession{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseRemoteSession(value string) (RemoteSession, error) {
	if value == "" {
		return RemoteSession{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return RemoteSession{}, fmt.Errorf("%w: expected two segments (remote_session:<uuid>)", ErrInvalid)
	}

	if parts[0] != "remote_session" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return RemoteSession{}, fmt.Errorf("%w: expected remote_session urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return RemoteSession{}, fmt.Errorf("%w: invalid remote_session uuid", ErrInvalid)
	}

	return NewRemoteSession(id), nil
}

func (u RemoteSession) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u RemoteSession) String() string {
	return "remote_session" + delimiter + u.ID.String()
}

func (u RemoteSession) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("remote_session urn to json: %w", err)
	}

	return b, nil
}

func (u *RemoteSession) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read remote_session urn string from json: %w", err)
	}

	parsed, err := ParseRemoteSession(s)
	if err != nil {
		return fmt.Errorf("parse remote_session urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *RemoteSession) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into RemoteSession", value)
	}

	parsed, err := ParseRemoteSession(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u RemoteSession) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u RemoteSession) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal remote_session urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *RemoteSession) UnmarshalText(text []byte) error {
	parsed, err := ParseRemoteSession(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal remote_session urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *RemoteSession) validate() error {
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
