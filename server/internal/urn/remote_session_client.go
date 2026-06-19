package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type RemoteSessionClient struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewRemoteSessionClient(id uuid.UUID) RemoteSessionClient {
	a := RemoteSessionClient{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseRemoteSessionClient(value string) (RemoteSessionClient, error) {
	if value == "" {
		return RemoteSessionClient{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return RemoteSessionClient{}, fmt.Errorf("%w: expected two segments (remote_session_client:<uuid>)", ErrInvalid)
	}

	if parts[0] != "remote_session_client" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return RemoteSessionClient{}, fmt.Errorf("%w: expected remote_session_client urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return RemoteSessionClient{}, fmt.Errorf("%w: invalid remote_session_client uuid", ErrInvalid)
	}

	return NewRemoteSessionClient(id), nil
}

func (u RemoteSessionClient) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u RemoteSessionClient) String() string {
	return "remote_session_client" + delimiter + u.ID.String()
}

func (u RemoteSessionClient) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("remote_session_client urn to json: %w", err)
	}

	return b, nil
}

func (u *RemoteSessionClient) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read remote_session_client urn string from json: %w", err)
	}

	parsed, err := ParseRemoteSessionClient(s)
	if err != nil {
		return fmt.Errorf("parse remote_session_client urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *RemoteSessionClient) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into RemoteSessionClient", value)
	}

	parsed, err := ParseRemoteSessionClient(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u RemoteSessionClient) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u RemoteSessionClient) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal remote_session_client urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *RemoteSessionClient) UnmarshalText(text []byte) error {
	parsed, err := ParseRemoteSessionClient(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal remote_session_client urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *RemoteSessionClient) validate() error {
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
