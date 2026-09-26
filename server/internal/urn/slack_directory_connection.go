package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type SlackDirectoryConnection struct {
	ID uuid.UUID
}

func NewSlackDirectoryConnection(id uuid.UUID) SlackDirectoryConnection {
	return SlackDirectoryConnection{ID: id}
}

func ParseSlackDirectoryConnection(value string) (SlackDirectoryConnection, error) {
	if value == "" {
		return SlackDirectoryConnection{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return SlackDirectoryConnection{}, fmt.Errorf("%w: expected two segments (slack_directory_connection:<uuid>)", ErrInvalid)
	}

	if parts[0] != "slack_directory_connection" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return SlackDirectoryConnection{}, fmt.Errorf("%w: expected slack_directory_connection urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return SlackDirectoryConnection{}, fmt.Errorf("%w: invalid slack_directory_connection uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return SlackDirectoryConnection{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewSlackDirectoryConnection(id), nil
}

func (u SlackDirectoryConnection) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u SlackDirectoryConnection) String() string {
	return "slack_directory_connection" + delimiter + u.ID.String()
}

func (u SlackDirectoryConnection) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("slack_directory_connection urn to json: %w", err)
	}

	return b, nil
}

func (u *SlackDirectoryConnection) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read slack_directory_connection urn string from json: %w", err)
	}

	parsed, err := ParseSlackDirectoryConnection(s)
	if err != nil {
		return fmt.Errorf("parse slack_directory_connection urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SlackDirectoryConnection) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into SlackDirectoryConnection", value)
	}

	parsed, err := ParseSlackDirectoryConnection(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u SlackDirectoryConnection) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u SlackDirectoryConnection) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal slack_directory_connection urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *SlackDirectoryConnection) UnmarshalText(text []byte) error {
	parsed, err := ParseSlackDirectoryConnection(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal slack_directory_connection urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u SlackDirectoryConnection) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
