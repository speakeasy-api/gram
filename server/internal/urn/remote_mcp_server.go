package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type RemoteMcpServer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewRemoteMcpServer(id uuid.UUID) RemoteMcpServer {
	a := RemoteMcpServer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseRemoteMcpServer(value string) (RemoteMcpServer, error) {
	if value == "" {
		return RemoteMcpServer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return RemoteMcpServer{}, fmt.Errorf("%w: expected two segments (remote-mcp-server:<uuid>)", ErrInvalid)
	}

	if parts[0] != "remote-mcp-server" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return RemoteMcpServer{}, fmt.Errorf("%w: expected remote-mcp-server urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return RemoteMcpServer{}, fmt.Errorf("%w: invalid remote-mcp-server uuid", ErrInvalid)
	}

	return NewRemoteMcpServer(id), nil
}

func (u RemoteMcpServer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u RemoteMcpServer) String() string {
	return "remote-mcp-server" + delimiter + u.ID.String()
}

func (u RemoteMcpServer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("remote-mcp-server urn to json: %w", err)
	}

	return b, nil
}

func (u *RemoteMcpServer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read remote-mcp-server urn string from json: %w", err)
	}

	parsed, err := ParseRemoteMcpServer(s)
	if err != nil {
		return fmt.Errorf("parse remote-mcp-server urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *RemoteMcpServer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into RemoteMcpServer", value)
	}

	parsed, err := ParseRemoteMcpServer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u RemoteMcpServer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u RemoteMcpServer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal remote-mcp-server urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *RemoteMcpServer) UnmarshalText(text []byte) error {
	parsed, err := ParseRemoteMcpServer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal remote-mcp-server urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *RemoteMcpServer) validate() error {
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
