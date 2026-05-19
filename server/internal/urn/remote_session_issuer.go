package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type RemoteSessionIssuer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewRemoteSessionIssuer(id uuid.UUID) RemoteSessionIssuer {
	a := RemoteSessionIssuer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseRemoteSessionIssuer(value string) (RemoteSessionIssuer, error) {
	if value == "" {
		return RemoteSessionIssuer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return RemoteSessionIssuer{}, fmt.Errorf("%w: expected two segments (remote_session_issuer:<uuid>)", ErrInvalid)
	}

	if parts[0] != "remote_session_issuer" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return RemoteSessionIssuer{}, fmt.Errorf("%w: expected remote_session_issuer urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return RemoteSessionIssuer{}, fmt.Errorf("%w: invalid remote_session_issuer uuid", ErrInvalid)
	}

	return NewRemoteSessionIssuer(id), nil
}

func (u RemoteSessionIssuer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u RemoteSessionIssuer) String() string {
	return "remote_session_issuer" + delimiter + u.ID.String()
}

func (u RemoteSessionIssuer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("remote_session_issuer urn to json: %w", err)
	}

	return b, nil
}

func (u *RemoteSessionIssuer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read remote_session_issuer urn string from json: %w", err)
	}

	parsed, err := ParseRemoteSessionIssuer(s)
	if err != nil {
		return fmt.Errorf("parse remote_session_issuer urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *RemoteSessionIssuer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into RemoteSessionIssuer", value)
	}

	parsed, err := ParseRemoteSessionIssuer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u RemoteSessionIssuer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u RemoteSessionIssuer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal remote_session_issuer urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *RemoteSessionIssuer) UnmarshalText(text []byte) error {
	parsed, err := ParseRemoteSessionIssuer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal remote_session_issuer urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *RemoteSessionIssuer) validate() error {
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
