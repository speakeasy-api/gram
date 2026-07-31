package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type PassthroughMcpServer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewPassthroughMcpServer(id uuid.UUID) PassthroughMcpServer {
	a := PassthroughMcpServer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParsePassthroughMcpServer(value string) (PassthroughMcpServer, error) {
	if value == "" {
		return PassthroughMcpServer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return PassthroughMcpServer{}, fmt.Errorf("%w: expected two segments (passthrough-mcp-server:<uuid>)", ErrInvalid)
	}

	if parts[0] != "passthrough-mcp-server" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return PassthroughMcpServer{}, fmt.Errorf("%w: expected passthrough-mcp-server urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return PassthroughMcpServer{}, fmt.Errorf("%w: invalid passthrough-mcp-server uuid", ErrInvalid)
	}

	return NewPassthroughMcpServer(id), nil
}

func (u PassthroughMcpServer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u PassthroughMcpServer) String() string {
	return "passthrough-mcp-server" + delimiter + u.ID.String()
}

func (u PassthroughMcpServer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("passthrough-mcp-server urn to json: %w", err)
	}

	return b, nil
}

func (u *PassthroughMcpServer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read passthrough-mcp-server urn string from json: %w", err)
	}

	parsed, err := ParsePassthroughMcpServer(s)
	if err != nil {
		return fmt.Errorf("parse passthrough-mcp-server urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *PassthroughMcpServer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into PassthroughMcpServer", value)
	}

	parsed, err := ParsePassthroughMcpServer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u PassthroughMcpServer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u PassthroughMcpServer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal passthrough-mcp-server urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *PassthroughMcpServer) UnmarshalText(text []byte) error {
	parsed, err := ParsePassthroughMcpServer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal passthrough-mcp-server urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *PassthroughMcpServer) validate() error {
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
