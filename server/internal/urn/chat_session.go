package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ChatSession is the URN for a managed chat session (the chats table). It is
// used by the audit log to identify chat-session access events.
type ChatSession struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewChatSession(id uuid.UUID) ChatSession {
	c := ChatSession{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = c.validate()

	return c
}

func ParseChatSession(value string) (ChatSession, error) {
	if value == "" {
		return ChatSession{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return ChatSession{}, fmt.Errorf("%w: expected two segments (chat:<uuid>)", ErrInvalid)
	}

	if parts[0] != "chat" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return ChatSession{}, fmt.Errorf("%w: expected chat urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return ChatSession{}, fmt.Errorf("%w: invalid chat uuid", ErrInvalid)
	}

	return NewChatSession(id), nil
}

func (u ChatSession) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u ChatSession) String() string {
	return "chat" + delimiter + u.ID.String()
}

func (u ChatSession) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("chat urn to json: %w", err)
	}

	return b, nil
}

func (u *ChatSession) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read chat urn string from json: %w", err)
	}

	parsed, err := ParseChatSession(s)
	if err != nil {
		return fmt.Errorf("parse chat urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *ChatSession) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into ChatSession", value)
	}

	parsed, err := ParseChatSession(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u ChatSession) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u ChatSession) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal chat urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *ChatSession) UnmarshalText(text []byte) error {
	parsed, err := ParseChatSession(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal chat urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *ChatSession) validate() error {
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
