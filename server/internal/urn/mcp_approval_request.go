package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type MCPApprovalRequest struct {
	ID uuid.UUID
}

func NewMCPApprovalRequest(id uuid.UUID) MCPApprovalRequest {
	return MCPApprovalRequest{ID: id}
}

func ParseMCPApprovalRequest(value string) (MCPApprovalRequest, error) {
	if value == "" {
		return MCPApprovalRequest{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return MCPApprovalRequest{}, fmt.Errorf("%w: expected two segments (mcp-approval-request:<uuid>)", ErrInvalid)
	}

	if parts[0] != "mcp-approval-request" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return MCPApprovalRequest{}, fmt.Errorf("%w: expected mcp-approval-request urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return MCPApprovalRequest{}, fmt.Errorf("%w: invalid mcp-approval-request uuid", ErrInvalid)
	}

	// The zero UUID is invalid for this type, so a URN carrying it is
	// rejected here rather than deferred to a later marshal or write.
	if id == uuid.Nil {
		return MCPApprovalRequest{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewMCPApprovalRequest(id), nil
}

func (u MCPApprovalRequest) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u MCPApprovalRequest) String() string {
	return "mcp-approval-request" + delimiter + u.ID.String()
}

func (u MCPApprovalRequest) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("mcp-approval-request urn to json: %w", err)
	}

	return b, nil
}

func (u *MCPApprovalRequest) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read mcp-approval-request urn string from json: %w", err)
	}

	parsed, err := ParseMCPApprovalRequest(s)
	if err != nil {
		return fmt.Errorf("parse mcp-approval-request urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *MCPApprovalRequest) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into MCPApprovalRequest", value)
	}

	parsed, err := ParseMCPApprovalRequest(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u MCPApprovalRequest) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u MCPApprovalRequest) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal mcp-approval-request urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *MCPApprovalRequest) UnmarshalText(text []byte) error {
	parsed, err := ParseMCPApprovalRequest(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal mcp-approval-request urn text: %w", err)
	}

	*u = parsed

	return nil
}

// validate checks the current ID on every call rather than caching a result,
// so a value whose exported ID was changed after construction is judged as it
// stands now.
func (u MCPApprovalRequest) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
