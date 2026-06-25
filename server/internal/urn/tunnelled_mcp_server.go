package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type TunnelledMcpServer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewTunnelledMcpServer(id uuid.UUID) TunnelledMcpServer {
	a := TunnelledMcpServer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseTunnelledMcpServer(value string) (TunnelledMcpServer, error) {
	if value == "" {
		return TunnelledMcpServer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return TunnelledMcpServer{}, fmt.Errorf("%w: expected two segments (tunnelled-mcp-server:<uuid>)", ErrInvalid)
	}

	if parts[0] != "tunnelled-mcp-server" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return TunnelledMcpServer{}, fmt.Errorf("%w: expected tunnelled-mcp-server urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return TunnelledMcpServer{}, fmt.Errorf("%w: invalid tunnelled-mcp-server uuid", ErrInvalid)
	}

	return NewTunnelledMcpServer(id), nil
}

func (u TunnelledMcpServer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u TunnelledMcpServer) String() string {
	return "tunnelled-mcp-server" + delimiter + u.ID.String()
}

func (u TunnelledMcpServer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("tunnelled-mcp-server urn to json: %w", err)
	}

	return b, nil
}

func (u *TunnelledMcpServer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read tunnelled-mcp-server urn string from json: %w", err)
	}

	parsed, err := ParseTunnelledMcpServer(s)
	if err != nil {
		return fmt.Errorf("parse tunnelled-mcp-server urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TunnelledMcpServer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into TunnelledMcpServer", value)
	}

	parsed, err := ParseTunnelledMcpServer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u TunnelledMcpServer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u TunnelledMcpServer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal tunnelled-mcp-server urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *TunnelledMcpServer) UnmarshalText(text []byte) error {
	parsed, err := ParseTunnelledMcpServer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal tunnelled-mcp-server urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TunnelledMcpServer) validate() error {
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
