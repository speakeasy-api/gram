package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type GoogleChatConnection struct {
	ID uuid.UUID
}

func NewGoogleChatConnection(id uuid.UUID) GoogleChatConnection {
	return GoogleChatConnection{ID: id}
}

func ParseGoogleChatConnection(value string) (GoogleChatConnection, error) {
	if value == "" {
		return GoogleChatConnection{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return GoogleChatConnection{}, fmt.Errorf("%w: expected two segments (google_chat_connection:<uuid>)", ErrInvalid)
	}

	if parts[0] != "google_chat_connection" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return GoogleChatConnection{}, fmt.Errorf("%w: expected google_chat_connection urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return GoogleChatConnection{}, fmt.Errorf("%w: invalid google_chat_connection uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return GoogleChatConnection{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewGoogleChatConnection(id), nil
}

func (u GoogleChatConnection) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u GoogleChatConnection) String() string {
	return "google_chat_connection" + delimiter + u.ID.String()
}

func (u GoogleChatConnection) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("google_chat_connection urn to json: %w", err)
	}

	return b, nil
}

func (u *GoogleChatConnection) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read google_chat_connection urn string from json: %w", err)
	}

	parsed, err := ParseGoogleChatConnection(s)
	if err != nil {
		return fmt.Errorf("parse google_chat_connection urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *GoogleChatConnection) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into GoogleChatConnection", value)
	}

	parsed, err := ParseGoogleChatConnection(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u GoogleChatConnection) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u GoogleChatConnection) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal google_chat_connection urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *GoogleChatConnection) UnmarshalText(text []byte) error {
	parsed, err := ParseGoogleChatConnection(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal google_chat_connection urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u GoogleChatConnection) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
