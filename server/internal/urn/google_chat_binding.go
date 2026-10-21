package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type GoogleChatBinding struct {
	ID uuid.UUID
}

func NewGoogleChatBinding(id uuid.UUID) GoogleChatBinding {
	return GoogleChatBinding{ID: id}
}

func ParseGoogleChatBinding(value string) (GoogleChatBinding, error) {
	if value == "" {
		return GoogleChatBinding{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return GoogleChatBinding{}, fmt.Errorf("%w: expected two segments (google_chat_binding:<uuid>)", ErrInvalid)
	}

	if parts[0] != "google_chat_binding" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return GoogleChatBinding{}, fmt.Errorf("%w: expected google_chat_binding urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return GoogleChatBinding{}, fmt.Errorf("%w: invalid google_chat_binding uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return GoogleChatBinding{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewGoogleChatBinding(id), nil
}

func (u GoogleChatBinding) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u GoogleChatBinding) String() string {
	return "google_chat_binding" + delimiter + u.ID.String()
}

func (u GoogleChatBinding) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("google_chat_binding urn to json: %w", err)
	}

	return b, nil
}

func (u *GoogleChatBinding) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read google_chat_binding urn string from json: %w", err)
	}

	parsed, err := ParseGoogleChatBinding(s)
	if err != nil {
		return fmt.Errorf("parse google_chat_binding urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *GoogleChatBinding) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into GoogleChatBinding", value)
	}

	parsed, err := ParseGoogleChatBinding(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u GoogleChatBinding) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u GoogleChatBinding) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal google_chat_binding urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *GoogleChatBinding) UnmarshalText(text []byte) error {
	parsed, err := ParseGoogleChatBinding(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal google_chat_binding urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u GoogleChatBinding) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
