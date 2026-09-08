package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type MetaMcpServerMember struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewMetaMcpServerMember(id uuid.UUID) MetaMcpServerMember {
	a := MetaMcpServerMember{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseMetaMcpServerMember(value string) (MetaMcpServerMember, error) {
	if value == "" {
		return MetaMcpServerMember{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return MetaMcpServerMember{}, fmt.Errorf("%w: expected two segments (meta-mcp-server-member:<uuid>)", ErrInvalid)
	}

	if parts[0] != "meta-mcp-server-member" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return MetaMcpServerMember{}, fmt.Errorf("%w: expected meta-mcp-server-member urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return MetaMcpServerMember{}, fmt.Errorf("%w: invalid meta-mcp-server-member uuid", ErrInvalid)
	}

	return NewMetaMcpServerMember(id), nil
}

func (u MetaMcpServerMember) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u MetaMcpServerMember) String() string {
	return "meta-mcp-server-member" + delimiter + u.ID.String()
}

func (u MetaMcpServerMember) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("meta-mcp-server-member urn to json: %w", err)
	}

	return b, nil
}

func (u *MetaMcpServerMember) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read meta-mcp-server-member urn string from json: %w", err)
	}

	parsed, err := ParseMetaMcpServerMember(s)
	if err != nil {
		return fmt.Errorf("parse meta-mcp-server-member urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *MetaMcpServerMember) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into MetaMcpServerMember", value)
	}

	parsed, err := ParseMetaMcpServerMember(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u MetaMcpServerMember) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u MetaMcpServerMember) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal meta-mcp-server-member urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *MetaMcpServerMember) UnmarshalText(text []byte) error {
	parsed, err := ParseMetaMcpServerMember(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal meta-mcp-server-member urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *MetaMcpServerMember) validate() error {
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
