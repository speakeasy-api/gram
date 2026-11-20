package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type ShadowMCPOfficialServer struct {
	ID uuid.UUID
}

func NewShadowMCPOfficialServer(id uuid.UUID) ShadowMCPOfficialServer {
	return ShadowMCPOfficialServer{ID: id}
}

func ParseShadowMCPOfficialServer(value string) (ShadowMCPOfficialServer, error) {
	if value == "" {
		return ShadowMCPOfficialServer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return ShadowMCPOfficialServer{}, fmt.Errorf("%w: expected two segments (shadow_mcp_official_server:<uuid>)", ErrInvalid)
	}

	if parts[0] != "shadow_mcp_official_server" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return ShadowMCPOfficialServer{}, fmt.Errorf("%w: expected shadow mcp official server urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return ShadowMCPOfficialServer{}, fmt.Errorf("%w: invalid shadow mcp official server uuid", ErrInvalid)
	}

	// This type treats the nil uuid as invalid, so it must not survive a
	// parse and turn up as a valid-looking urn later.
	if id == uuid.Nil {
		return ShadowMCPOfficialServer{}, fmt.Errorf("%w: empty shadow mcp official server uuid", ErrInvalid)
	}

	return NewShadowMCPOfficialServer(id), nil
}

func (u ShadowMCPOfficialServer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u ShadowMCPOfficialServer) String() string {
	return "shadow_mcp_official_server" + delimiter + u.ID.String()
}

func (u ShadowMCPOfficialServer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("shadow mcp official server urn to json: %w", err)
	}

	return b, nil
}

func (u *ShadowMCPOfficialServer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read shadow mcp official server urn string from json: %w", err)
	}

	parsed, err := ParseShadowMCPOfficialServer(s)
	if err != nil {
		return fmt.Errorf("parse shadow mcp official server urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *ShadowMCPOfficialServer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into ShadowMCPOfficialServer", value)
	}

	parsed, err := ParseShadowMCPOfficialServer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u ShadowMCPOfficialServer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u ShadowMCPOfficialServer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal shadow mcp official server urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *ShadowMCPOfficialServer) UnmarshalText(text []byte) error {
	parsed, err := ParseShadowMCPOfficialServer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal shadow mcp official server urn text: %w", err)
	}

	*u = parsed

	return nil
}

// validate reads the current ID every call. ID is exported and mutable, so a
// cached verdict could outlive the value it judged and report a zeroed urn as
// valid.
func (u *ShadowMCPOfficialServer) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
