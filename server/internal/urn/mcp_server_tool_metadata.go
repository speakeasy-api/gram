package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// McpServerToolMetadata identifies an MCP server's tool metadata collection as
// a whole, so ID is the MCP server's id rather than the id of any individual
// mcp_server_tool_metadata row. Tool metadata is only ever read and written per
// server, and auditing it per row would be noise.
type McpServerToolMetadata struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewMcpServerToolMetadata(mcpServerID uuid.UUID) McpServerToolMetadata {
	a := McpServerToolMetadata{
		ID:      mcpServerID,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseMcpServerToolMetadata(value string) (McpServerToolMetadata, error) {
	if value == "" {
		return McpServerToolMetadata{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return McpServerToolMetadata{}, fmt.Errorf("%w: expected two segments (mcp-server-tool-metadata:<uuid>)", ErrInvalid)
	}

	if parts[0] != "mcp-server-tool-metadata" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return McpServerToolMetadata{}, fmt.Errorf("%w: expected mcp-server-tool-metadata urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return McpServerToolMetadata{}, fmt.Errorf("%w: invalid mcp-server-tool-metadata uuid", ErrInvalid)
	}

	return NewMcpServerToolMetadata(id), nil
}

func (u McpServerToolMetadata) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u McpServerToolMetadata) String() string {
	return "mcp-server-tool-metadata" + delimiter + u.ID.String()
}

func (u McpServerToolMetadata) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("mcp-server-tool-metadata urn to json: %w", err)
	}

	return b, nil
}

func (u *McpServerToolMetadata) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read mcp-server-tool-metadata urn string from json: %w", err)
	}

	parsed, err := ParseMcpServerToolMetadata(s)
	if err != nil {
		return fmt.Errorf("parse mcp-server-tool-metadata urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpServerToolMetadata) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into McpServerToolMetadata", value)
	}

	parsed, err := ParseMcpServerToolMetadata(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u McpServerToolMetadata) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u McpServerToolMetadata) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal mcp-server-tool-metadata urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *McpServerToolMetadata) UnmarshalText(text []byte) error {
	parsed, err := ParseMcpServerToolMetadata(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal mcp-server-tool-metadata urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpServerToolMetadata) validate() error {
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
