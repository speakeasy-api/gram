package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type MetaMcpServer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewMetaMcpServer(id uuid.UUID) MetaMcpServer {
	a := MetaMcpServer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseMetaMcpServer(value string) (MetaMcpServer, error) {
	if value == "" {
		return MetaMcpServer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return MetaMcpServer{}, fmt.Errorf("%w: expected two segments (meta-mcp-server:<uuid>)", ErrInvalid)
	}

	if parts[0] != "meta-mcp-server" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return MetaMcpServer{}, fmt.Errorf("%w: expected meta-mcp-server urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return MetaMcpServer{}, fmt.Errorf("%w: invalid meta-mcp-server uuid", ErrInvalid)
	}

	return NewMetaMcpServer(id), nil
}

func (u MetaMcpServer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u MetaMcpServer) String() string {
	return "meta-mcp-server" + delimiter + u.ID.String()
}

func (u MetaMcpServer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("meta-mcp-server urn to json: %w", err)
	}

	return b, nil
}

func (u *MetaMcpServer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read meta-mcp-server urn string from json: %w", err)
	}

	parsed, err := ParseMetaMcpServer(s)
	if err != nil {
		return fmt.Errorf("parse meta-mcp-server urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *MetaMcpServer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into MetaMcpServer", value)
	}

	parsed, err := ParseMetaMcpServer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u MetaMcpServer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u MetaMcpServer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal meta-mcp-server urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *MetaMcpServer) UnmarshalText(text []byte) error {
	parsed, err := ParseMetaMcpServer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal meta-mcp-server urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *MetaMcpServer) validate() error {
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
