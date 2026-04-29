package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type McpEndpoint struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewMcpEndpoint(id uuid.UUID) McpEndpoint {
	a := McpEndpoint{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseMcpEndpoint(value string) (McpEndpoint, error) {
	if value == "" {
		return McpEndpoint{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return McpEndpoint{}, fmt.Errorf("%w: expected two segments (mcp-endpoint:<uuid>)", ErrInvalid)
	}

	if parts[0] != "mcp-endpoint" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return McpEndpoint{}, fmt.Errorf("%w: expected mcp-endpoint urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return McpEndpoint{}, fmt.Errorf("%w: invalid mcp-endpoint uuid", ErrInvalid)
	}

	return NewMcpEndpoint(id), nil
}

func (u McpEndpoint) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u McpEndpoint) String() string {
	return "mcp-endpoint" + delimiter + u.ID.String()
}

func (u McpEndpoint) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("mcp-endpoint urn to json: %w", err)
	}

	return b, nil
}

func (u *McpEndpoint) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read mcp-endpoint urn string from json: %w", err)
	}

	parsed, err := ParseMcpEndpoint(s)
	if err != nil {
		return fmt.Errorf("parse mcp-endpoint urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpEndpoint) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into McpEndpoint", value)
	}

	parsed, err := ParseMcpEndpoint(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u McpEndpoint) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u McpEndpoint) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal mcp-endpoint urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *McpEndpoint) UnmarshalText(text []byte) error {
	parsed, err := ParseMcpEndpoint(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal mcp-endpoint urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpEndpoint) validate() error {
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
