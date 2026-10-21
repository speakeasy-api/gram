package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type GoogleChatMember struct {
	ID uuid.UUID
}

func NewGoogleChatMember(id uuid.UUID) GoogleChatMember {
	return GoogleChatMember{ID: id}
}

func ParseGoogleChatMember(value string) (GoogleChatMember, error) {
	if value == "" {
		return GoogleChatMember{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return GoogleChatMember{}, fmt.Errorf("%w: expected two segments (google_chat_member:<uuid>)", ErrInvalid)
	}

	if parts[0] != "google_chat_member" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return GoogleChatMember{}, fmt.Errorf("%w: expected google_chat_member urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return GoogleChatMember{}, fmt.Errorf("%w: invalid google_chat_member uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return GoogleChatMember{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewGoogleChatMember(id), nil
}

func (u GoogleChatMember) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u GoogleChatMember) String() string {
	return "google_chat_member" + delimiter + u.ID.String()
}

func (u GoogleChatMember) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("google_chat_member urn to json: %w", err)
	}

	return b, nil
}

func (u *GoogleChatMember) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read google_chat_member urn string from json: %w", err)
	}

	parsed, err := ParseGoogleChatMember(s)
	if err != nil {
		return fmt.Errorf("parse google_chat_member urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *GoogleChatMember) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into GoogleChatMember", value)
	}

	parsed, err := ParseGoogleChatMember(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u GoogleChatMember) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u GoogleChatMember) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal google_chat_member urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *GoogleChatMember) UnmarshalText(text []byte) error {
	parsed, err := ParseGoogleChatMember(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal google_chat_member urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u GoogleChatMember) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
