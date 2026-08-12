package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type LiteLLMInstance struct {
	ID uuid.UUID
}

func NewLiteLLMInstance(id uuid.UUID) LiteLLMInstance {
	return LiteLLMInstance{ID: id}
}

func ParseLiteLLMInstance(value string) (LiteLLMInstance, error) {
	if value == "" {
		return LiteLLMInstance{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}
	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return LiteLLMInstance{}, fmt.Errorf("%w: expected two segments (litellm-instance:<uuid>)", ErrInvalid)
	}
	if parts[0] != "litellm-instance" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return LiteLLMInstance{}, fmt.Errorf("%w: expected litellm-instance urn (got: %q)", ErrInvalid, truncated)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return LiteLLMInstance{}, fmt.Errorf("%w: invalid litellm-instance uuid", ErrInvalid)
	}
	return NewLiteLLMInstance(id), nil
}

func (u LiteLLMInstance) IsZero() bool   { return u.ID == uuid.Nil }
func (u LiteLLMInstance) String() string { return "litellm-instance" + delimiter + u.ID.String() }

func (u LiteLLMInstance) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("litellm-instance urn to json: %w", err)
	}
	return b, nil
}

func (u *LiteLLMInstance) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read litellm-instance urn string from json: %w", err)
	}
	parsed, err := ParseLiteLLMInstance(s)
	if err != nil {
		return fmt.Errorf("parse litellm-instance urn json string: %w", err)
	}
	*u = parsed
	return nil
}

func (u *LiteLLMInstance) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into LiteLLMInstance", value)
	}
	parsed, err := ParseLiteLLMInstance(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}
	*u = parsed
	return nil
}

func (u LiteLLMInstance) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	return u.String(), nil
}

func (u LiteLLMInstance) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal litellm-instance urn text: %w", err)
	}
	return []byte(u.String()), nil
}

func (u *LiteLLMInstance) UnmarshalText(text []byte) error {
	parsed, err := ParseLiteLLMInstance(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal litellm-instance urn text: %w", err)
	}
	*u = parsed
	return nil
}

func (u LiteLLMInstance) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}
	return nil
}
