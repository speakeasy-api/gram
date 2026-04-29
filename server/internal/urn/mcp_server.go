package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type McpServer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewMcpServer(id uuid.UUID) McpServer {
	a := McpServer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseMcpServer(value string) (McpServer, error) {
	if value == "" {
		return McpServer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return McpServer{}, fmt.Errorf("%w: expected two segments (mcp-server:<uuid>)", ErrInvalid)
	}

	if parts[0] != "mcp-server" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return McpServer{}, fmt.Errorf("%w: expected mcp-server urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return McpServer{}, fmt.Errorf("%w: invalid mcp-server uuid", ErrInvalid)
	}

	return NewMcpServer(id), nil
}

func (u McpServer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u McpServer) String() string {
	return "mcp-server" + delimiter + u.ID.String()
}

func (u McpServer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("mcp-server urn to json: %w", err)
	}

	return b, nil
}

func (u *McpServer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read mcp-server urn string from json: %w", err)
	}

	parsed, err := ParseMcpServer(s)
	if err != nil {
		return fmt.Errorf("parse mcp-server urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpServer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into McpServer", value)
	}

	parsed, err := ParseMcpServer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u McpServer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u McpServer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal mcp-server urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *McpServer) UnmarshalText(text []byte) error {
	parsed, err := ParseMcpServer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal mcp-server urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *McpServer) validate() error {
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
