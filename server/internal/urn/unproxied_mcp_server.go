package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type UnproxiedMcpServer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewUnproxiedMcpServer(id uuid.UUID) UnproxiedMcpServer {
	a := UnproxiedMcpServer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseUnproxiedMcpServer(value string) (UnproxiedMcpServer, error) {
	if value == "" {
		return UnproxiedMcpServer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return UnproxiedMcpServer{}, fmt.Errorf("%w: expected two segments (unproxied-mcp-server:<uuid>)", ErrInvalid)
	}

	if parts[0] != "unproxied-mcp-server" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return UnproxiedMcpServer{}, fmt.Errorf("%w: expected unproxied-mcp-server urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return UnproxiedMcpServer{}, fmt.Errorf("%w: invalid unproxied-mcp-server uuid", ErrInvalid)
	}

	return NewUnproxiedMcpServer(id), nil
}

func (u UnproxiedMcpServer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u UnproxiedMcpServer) String() string {
	return "unproxied-mcp-server" + delimiter + u.ID.String()
}

func (u UnproxiedMcpServer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("unproxied-mcp-server urn to json: %w", err)
	}

	return b, nil
}

func (u *UnproxiedMcpServer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read unproxied-mcp-server urn string from json: %w", err)
	}

	parsed, err := ParseUnproxiedMcpServer(s)
	if err != nil {
		return fmt.Errorf("parse unproxied-mcp-server urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UnproxiedMcpServer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into UnproxiedMcpServer", value)
	}

	parsed, err := ParseUnproxiedMcpServer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u UnproxiedMcpServer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u UnproxiedMcpServer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal unproxied-mcp-server urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *UnproxiedMcpServer) UnmarshalText(text []byte) error {
	parsed, err := ParseUnproxiedMcpServer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal unproxied-mcp-server urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *UnproxiedMcpServer) validate() error {
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
