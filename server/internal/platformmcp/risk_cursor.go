package platformmcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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
	key []byte
}

func newRiskCursorCodec(keyMaterial string) (*riskCursorCodec, error) {
	if keyMaterial == "" {
		return nil, ErrRiskCursorInvalid
	}
	key := sha256.Sum256([]byte("platform-mcp-risk-read-cursor:" + keyMaterial))
	return &riskCursorCodec{key: key[:]}, nil
}

func (c *riskCursorCodec) Encode(cursor riskCursor) (string, error) {
	if c == nil || len(c.key) == 0 || cursor.Kind == "" || cursor.OrganizationID == "" || cursor.Binding == "" || cursor.ProjectID == uuid.Nil || cursor.CreatedAt.IsZero() || cursor.ID == uuid.Nil {
		return "", ErrRiskCursorInvalid
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode platform mcp risk cursor: %w", err)
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	token := make([]byte, 0, len(payload)+sha256.Size)
	token = append(token, payload...)
	token = append(token, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

func (c *riskCursorCodec) Decode(value string, principal Principal, kind string, projectID, policyID uuid.UUID) (riskCursor, error) {
	incompleteConnection := (principal.ConnectionID == "") != (principal.Generation == "")
	binding := principalCursorBinding(principal)
	if c == nil || len(c.key) == 0 || value == "" || principal.OrganizationID == "" || incompleteConnection || binding == "" || kind == "" || projectID == uuid.Nil {
		return riskCursor{}, ErrRiskCursorInvalid
	}
	token, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(token) <= sha256.Size {
		return riskCursor{}, ErrRiskCursorInvalid
	}
	payload, signature := token[:len(token)-sha256.Size], token[len(token)-sha256.Size:]
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return riskCursor{}, ErrRiskCursorInvalid
	}
	var cursor riskCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Kind != kind || cursor.OrganizationID != principal.OrganizationID || cursor.Binding != binding || cursor.ProjectID != projectID || cursor.PolicyID != policyID || cursor.CreatedAt.IsZero() || cursor.ID == uuid.Nil {
		return riskCursor{}, ErrRiskCursorInvalid
	}
	return cursor, nil
}
