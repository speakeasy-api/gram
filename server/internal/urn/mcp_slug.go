package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type McpSlug struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewMcpSlug(id uuid.UUID) McpSlug {
	a := McpSlug{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseMcpSlug(value string) (McpSlug, error) {
	if value == "" {
		return McpSlug{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return McpSlug{}, fmt.Errorf("%w: expected two segments (mcp-slug:<uuid>)", ErrInvalid)
	}

	if parts[0] != "mcp-slug" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return McpSlug{}, fmt.Errorf("%w: expected mcp-slug urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return McpSlug{}, fmt.Errorf("%w: invalid mcp-slug uuid", ErrInvalid)
	}

	return NewMcpSlug(id), nil
}

func (u McpSlug) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u McpSlug) String() string {
	return "mcp-slug" + delimiter + u.ID.String()
}

func (u McpSlug) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("mcp-slug urn to json: %w", err)
	}

	return b, nil
}

func (u *McpSlug) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read mcp-slug urn string from json: %w", err)
	}

	parsed, err := ParseMcpSlug(s)
	if err != nil {
		return fmt.Errorf("parse mcp-slug urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpSlug) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into McpSlug", value)
	}

	parsed, err := ParseMcpSlug(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u McpSlug) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u McpSlug) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal mcp-slug urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *McpSlug) UnmarshalText(text []byte) error {
	parsed, err := ParseMcpSlug(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal mcp-slug urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpSlug) validate() error {
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
