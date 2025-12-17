package auth

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/functions/internal/encryption"
)

type authPayloadV1 struct {
	ID      string `json:"id"`
	Exp     int64  `json:"exp"`
	Subject string `json:"sub"`
}

func authorizeV1(enc *encryption.Client, ciphertext string) (*authPayloadV1, error) {
	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("not authorized to call gram functions")
	}

	var payload authPayloadV1
	if err := json.Unmarshal([]byte(plaintext), &payload); err != nil {
		return nil, fmt.Errorf("unmarshal bearer token: %w", err)
	}

	if payload.ID == "" {
		return nil, fmt.Errorf("invalid bearer token: missing id")
	}

	expTime := time.Unix(payload.Exp, 0)
	if time.Now().After(expTime) {
		return nil, fmt.Errorf("bearer token expired")
	}

	return &payload, nil
}
