package auth

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/functions/internal/encryption"
)

type authPayloadV1 struct {
	ID  string `json:"id"`
	Exp int64  `json:"exp"`
}

func authorizeV1(enc *encryption.Client, ciphertext string) error {
	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		return fmt.Errorf("not authorized to call gram functions")
	}

	var payload authPayloadV1
	if err := json.Unmarshal([]byte(plaintext), &payload); err != nil {
		return fmt.Errorf("unmarshal bearer token: %w", err)
	}

	if payload.ID == "" {
		return fmt.Errorf("invalid bearer token: missing id")
	}

	expTime := time.Unix(payload.Exp, 0)
	if time.Now().After(expTime) {
		return fmt.Errorf("bearer token expired")
	}

	return nil
}
