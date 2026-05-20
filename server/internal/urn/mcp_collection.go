package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type McpCollection struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewMcpCollection(id uuid.UUID) McpCollection {
	c := McpCollection{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = c.validate()

	return c
}

func ParseMcpCollection(value string) (McpCollection, error) {
	if value == "" {
		return McpCollection{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return McpCollection{}, fmt.Errorf("%w: expected two segments (mcp_collection:<uuid>)", ErrInvalid)
	}

	if parts[0] != "mcp_collection" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return McpCollection{}, fmt.Errorf("%w: expected mcp_collection urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return McpCollection{}, fmt.Errorf("%w: invalid mcp_collection uuid", ErrInvalid)
	}

	return NewMcpCollection(id), nil
}

func (u McpCollection) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u McpCollection) String() string {
	return "mcp_collection" + delimiter + u.ID.String()
}

func (u McpCollection) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("mcp_collection urn to json: %w", err)
	}

	return b, nil
}

func (u *McpCollection) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read mcp_collection urn string from json: %w", err)
	}

	parsed, err := ParseMcpCollection(s)
	if err != nil {
		return fmt.Errorf("parse mcp_collection urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpCollection) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into McpCollection", value)
	}

	parsed, err := ParseMcpCollection(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u McpCollection) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u McpCollection) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal mcp_collection urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *McpCollection) UnmarshalText(text []byte) error {
	parsed, err := ParseMcpCollection(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal mcp_collection urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpCollection) validate() error {
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
