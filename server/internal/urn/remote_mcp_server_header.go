package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type RemoteMcpServerHeader struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewRemoteMcpServerHeader(id uuid.UUID) RemoteMcpServerHeader {
	a := RemoteMcpServerHeader{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseRemoteMcpServerHeader(value string) (RemoteMcpServerHeader, error) {
	if value == "" {
		return RemoteMcpServerHeader{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return RemoteMcpServerHeader{}, fmt.Errorf("%w: expected two segments (remote-mcp-server-header:<uuid>)", ErrInvalid)
	}

	if parts[0] != "remote-mcp-server-header" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return RemoteMcpServerHeader{}, fmt.Errorf("%w: expected remote-mcp-server-header urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return RemoteMcpServerHeader{}, fmt.Errorf("%w: invalid remote-mcp-server-header uuid", ErrInvalid)
	}

	return NewRemoteMcpServerHeader(id), nil
}

func (u RemoteMcpServerHeader) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u RemoteMcpServerHeader) String() string {
	return "remote-mcp-server-header" + delimiter + u.ID.String()
}

func (u RemoteMcpServerHeader) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("remote-mcp-server-header urn to json: %w", err)
	}

	return b, nil
}

func (u *RemoteMcpServerHeader) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read remote-mcp-server-header urn string from json: %w", err)
	}

	parsed, err := ParseRemoteMcpServerHeader(s)
	if err != nil {
		return fmt.Errorf("parse remote-mcp-server-header urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *RemoteMcpServerHeader) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into RemoteMcpServerHeader", value)
	}

	parsed, err := ParseRemoteMcpServerHeader(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u RemoteMcpServerHeader) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u RemoteMcpServerHeader) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal remote-mcp-server-header urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *RemoteMcpServerHeader) UnmarshalText(text []byte) error {
	parsed, err := ParseRemoteMcpServerHeader(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal remote-mcp-server-header urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *RemoteMcpServerHeader) validate() error {
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
