package platformmcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
)

type shadowDecisionVersionCodec struct{ key []byte }

func newShadowDecisionVersionCodec(keyMaterial string) (*shadowDecisionVersionCodec, error) {
	if keyMaterial == "" {
		return nil, ErrShadowInventoryUnavailable
	}
	key := sha256.Sum256([]byte("platform-mcp-shadow-decision-version:" + keyMaterial))
	return &shadowDecisionVersionCodec{key: key[:]}, nil
}

func (c *shadowDecisionVersionCodec) Encode(state mcpapproval.DecisionVersionState) (string, error) {
	if c == nil || len(c.key) != sha256.Size || state.RequestID.String() == "" {
		return "", ErrShadowInventoryUnavailable
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode shadow decision version state: %w", err)
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (c *shadowDecisionVersionCodec) Match(expected string, state mcpapproval.DecisionVersionState) bool {
	actual, err := c.Encode(state)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(expected), []byte(actual))
}

var ErrShadowDecisionConflict = errors.New("platform mcp shadow decision conflict")
