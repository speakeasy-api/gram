package functions

import (
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/encryption"
)

type tokenRequestV1 struct {
	ID  string `json:"id"`
	Exp int64  `json:"exp"`
}

func tokenV1(enc *encryption.Client, req tokenRequestV1) (string, error) {
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
