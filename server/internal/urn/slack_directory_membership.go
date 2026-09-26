package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type SlackDirectoryMembership struct {
	ID uuid.UUID
}

func NewSlackDirectoryMembership(id uuid.UUID) SlackDirectoryMembership {
	return SlackDirectoryMembership{ID: id}
}

func ParseSlackDirectoryMembership(value string) (SlackDirectoryMembership, error) {
	if value == "" {
		return SlackDirectoryMembership{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return SlackDirectoryMembership{}, fmt.Errorf("%w: expected two segments (slack_directory_membership:<uuid>)", ErrInvalid)
	}

	if parts[0] != "slack_directory_membership" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return SlackDirectoryMembership{}, fmt.Errorf("%w: expected slack_directory_membership urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return SlackDirectoryMembership{}, fmt.Errorf("%w: invalid slack_directory_membership uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return SlackDirectoryMembership{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewSlackDirectoryMembership(id), nil
}

func (u SlackDirectoryMembership) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u SlackDirectoryMembership) String() string {
	return "slack_directory_membership" + delimiter + u.ID.String()
}

func (u SlackDirectoryMembership) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("slack_directory_membership urn to json: %w", err)
	}

	return b, nil
}

func (u *SlackDirectoryMembership) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read slack_directory_membership urn string from json: %w", err)
	}

	parsed, err := ParseSlackDirectoryMembership(s)
	if err != nil {
		return fmt.Errorf("parse slack_directory_membership urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SlackDirectoryMembership) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into SlackDirectoryMembership", value)
	}

	parsed, err := ParseSlackDirectoryMembership(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u SlackDirectoryMembership) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u SlackDirectoryMembership) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal slack_directory_membership urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *SlackDirectoryMembership) UnmarshalText(text []byte) error {
	parsed, err := ParseSlackDirectoryMembership(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal slack_directory_membership urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u SlackDirectoryMembership) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
