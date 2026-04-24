package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type McpFrontend struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewMcpFrontend(id uuid.UUID) McpFrontend {
	a := McpFrontend{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseMcpFrontend(value string) (McpFrontend, error) {
	if value == "" {
		return McpFrontend{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return McpFrontend{}, fmt.Errorf("%w: expected two segments (mcp-frontend:<uuid>)", ErrInvalid)
	}

	if parts[0] != "mcp-frontend" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return McpFrontend{}, fmt.Errorf("%w: expected mcp-frontend urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return McpFrontend{}, fmt.Errorf("%w: invalid mcp-frontend uuid", ErrInvalid)
	}

	return NewMcpFrontend(id), nil
}

func (u McpFrontend) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u McpFrontend) String() string {
	return "mcp-frontend" + delimiter + u.ID.String()
}

func (u McpFrontend) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("mcp-frontend urn to json: %w", err)
	}

	return b, nil
}

func (u *McpFrontend) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read mcp-frontend urn string from json: %w", err)
	}

	parsed, err := ParseMcpFrontend(s)
	if err != nil {
		return fmt.Errorf("parse mcp-frontend urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpFrontend) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into McpFrontend", value)
	}

	parsed, err := ParseMcpFrontend(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u McpFrontend) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u McpFrontend) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal mcp-frontend urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *McpFrontend) UnmarshalText(text []byte) error {
	parsed, err := ParseMcpFrontend(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal mcp-frontend urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpFrontend) validate() error {
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
