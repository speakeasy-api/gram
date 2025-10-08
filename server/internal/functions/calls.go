package functions

import (
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/encryption"
)

type TokenRequestV1 struct {
	ID  string `json:"id"`
	Exp int64  `json:"exp"`
}

func TokenV1(enc *encryption.Client, req TokenRequestV1) (string, error) {
	bs, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal v1 token: %w", err)
	}

	encBs, err := enc.Encrypt(bs)
	if err != nil {
		return "", fmt.Errorf("encrypt v1 token: %w", err)
	}

	return fmt.Sprintf("v01.%s", encBs), nil
}

type CallToolPayload struct {
	ToolName    string            `json:"name"`
	Input       json.RawMessage   `json:"input"`
	Environment map[string]string `json:"environment,omitempty,omitzero"`
}
