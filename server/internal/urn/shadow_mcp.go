package urn

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type ShadowMCPApprovalRequest struct {
	ID uuid.UUID
}

func NewShadowMCPApprovalRequest(id uuid.UUID) ShadowMCPApprovalRequest {
	return ShadowMCPApprovalRequest{ID: id}
}

func (u ShadowMCPApprovalRequest) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u ShadowMCPApprovalRequest) String() string {
	return "shadowmcpapprovalrequest" + delimiter + u.ID.String()
}

func (u ShadowMCPApprovalRequest) MarshalJSON() ([]byte, error) {
	if u.IsZero() {
		return nil, fmt.Errorf("%w: zero shadow mcp approval request urn", ErrInvalid)
	}
	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("shadow mcp approval request urn to json: %w", err)
	}
	return b, nil
}

type ShadowMCPAccessRule struct {
	ID uuid.UUID
}

func NewShadowMCPAccessRule(id uuid.UUID) ShadowMCPAccessRule {
	return ShadowMCPAccessRule{ID: id}
}

func (u ShadowMCPAccessRule) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u ShadowMCPAccessRule) String() string {
	return "shadowmcpaccessrule" + delimiter + u.ID.String()
}

func (u ShadowMCPAccessRule) MarshalJSON() ([]byte, error) {
	if u.IsZero() {
		return nil, fmt.Errorf("%w: zero shadow mcp access rule urn", ErrInvalid)
	}
	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("shadow mcp access rule urn to json: %w", err)
	}
	return b, nil
}
