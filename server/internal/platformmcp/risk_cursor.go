package platformmcp

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/inv"
)

var ErrRiskCursorInvalid = errors.New("invalid platform mcp risk cursor")

type riskCursor struct {
	Kind           string    `json:"kind"`
	OrganizationID string    `json:"organization_id"`
	Binding        string    `json:"binding"`
	ProjectID      uuid.UUID `json:"project_id"`
	PolicyID       uuid.UUID `json:"policy_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ID             uuid.UUID `json:"id"`
}

type riskCursorCodec struct {
	key signedCursorKey
}

func newRiskCursorCodec(keyMaterial string) *riskCursorCodec {
	inv.Require("platform mcp risk cursor codec", "key material is configured", keyMaterial != "")

	return &riskCursorCodec{key: newSignedCursorKey("platform-mcp-risk-read-cursor", keyMaterial)}
}

func (c *riskCursorCodec) Encode(cursor riskCursor) (string, error) {
	if c == nil || len(c.key) == 0 || cursor.Kind == "" || cursor.OrganizationID == "" || cursor.Binding == "" || cursor.ProjectID == uuid.Nil || cursor.CreatedAt.IsZero() || cursor.ID == uuid.Nil {
		return "", ErrRiskCursorInvalid
	}
	token, err := sealCursor(c.key, cursor)
	if err != nil {
		return "", fmt.Errorf("encode platform mcp risk cursor: %w", err)
	}
	return token, nil
}

func (c *riskCursorCodec) Decode(value string, principal Principal, kind string, projectID, policyID uuid.UUID) (riskCursor, error) {
	incompleteConnection := (principal.ConnectionID == "") != (principal.Generation == "")
	binding := principalCursorBinding(principal)
	if c == nil || len(c.key) == 0 || value == "" || principal.OrganizationID == "" || incompleteConnection || binding == "" || kind == "" || projectID == uuid.Nil {
		return riskCursor{}, ErrRiskCursorInvalid
	}
	cursor, ok := openCursor[riskCursor](c.key, value)
	if !ok || cursor.Kind != kind || cursor.OrganizationID != principal.OrganizationID || cursor.Binding != binding || cursor.ProjectID != projectID || cursor.PolicyID != policyID || cursor.CreatedAt.IsZero() || cursor.ID == uuid.Nil {
		return riskCursor{}, ErrRiskCursorInvalid
	}
	return cursor, nil
}
