package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type TunneledMcpServerHeader struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewTunneledMcpServerHeader(id uuid.UUID) TunneledMcpServerHeader {
	a := TunneledMcpServerHeader{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseTunneledMcpServerHeader(value string) (TunneledMcpServerHeader, error) {
	if value == "" {
		return TunneledMcpServerHeader{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return TunneledMcpServerHeader{}, fmt.Errorf("%w: expected two segments (tunneled-mcp-server-header:<uuid>)", ErrInvalid)
	}

	if parts[0] != "tunneled-mcp-server-header" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return TunneledMcpServerHeader{}, fmt.Errorf("%w: expected tunneled-mcp-server-header urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return TunneledMcpServerHeader{}, fmt.Errorf("%w: invalid tunneled-mcp-server-header uuid", ErrInvalid)
	}

	return NewTunneledMcpServerHeader(id), nil
}

func (u TunneledMcpServerHeader) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u TunneledMcpServerHeader) String() string {
	return "tunneled-mcp-server-header" + delimiter + u.ID.String()
}

func (u TunneledMcpServerHeader) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("tunneled-mcp-server-header urn to json: %w", err)
	}

	return b, nil
}

func (u *TunneledMcpServerHeader) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read tunneled-mcp-server-header urn string from json: %w", err)
	}

	parsed, err := ParseTunneledMcpServerHeader(s)
	if err != nil {
		return fmt.Errorf("parse tunneled-mcp-server-header urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TunneledMcpServerHeader) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into TunneledMcpServerHeader", value)
	}

	parsed, err := ParseTunneledMcpServerHeader(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u TunneledMcpServerHeader) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u TunneledMcpServerHeader) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal tunneled-mcp-server-header urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *TunneledMcpServerHeader) UnmarshalText(text []byte) error {
	parsed, err := ParseTunneledMcpServerHeader(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal tunneled-mcp-server-header urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TunneledMcpServerHeader) validate() error {
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
