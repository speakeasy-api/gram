package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type TunneledMcpServer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewTunneledMcpServer(id uuid.UUID) TunneledMcpServer {
	a := TunneledMcpServer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseTunneledMcpServer(value string) (TunneledMcpServer, error) {
	if value == "" {
		return TunneledMcpServer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return TunneledMcpServer{}, fmt.Errorf("%w: expected two segments (tunneled-mcp-server:<uuid>)", ErrInvalid)
	}

	if parts[0] != "tunneled-mcp-server" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return TunneledMcpServer{}, fmt.Errorf("%w: expected tunneled-mcp-server urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return TunneledMcpServer{}, fmt.Errorf("%w: invalid tunneled-mcp-server uuid", ErrInvalid)
	}

	return NewTunneledMcpServer(id), nil
}

func (u TunneledMcpServer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u TunneledMcpServer) String() string {
	return "tunneled-mcp-server" + delimiter + u.ID.String()
}

func (u TunneledMcpServer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("tunneled-mcp-server urn to json: %w", err)
	}

	return b, nil
}

func (u *TunneledMcpServer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read tunneled-mcp-server urn string from json: %w", err)
	}

	parsed, err := ParseTunneledMcpServer(s)
	if err != nil {
		return fmt.Errorf("parse tunneled-mcp-server urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TunneledMcpServer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into TunneledMcpServer", value)
	}

	parsed, err := ParseTunneledMcpServer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u TunneledMcpServer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u TunneledMcpServer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal tunneled-mcp-server urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *TunneledMcpServer) UnmarshalText(text []byte) error {
	parsed, err := ParseTunneledMcpServer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal tunneled-mcp-server urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TunneledMcpServer) validate() error {
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
