package keys

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

type APIKeyScopes string

const randomKeyLength = 64

const (
	APIKeyScopesConsumer APIKeyScopes = "consumer" // allows api key access a tool consumer
	APIKeyScopesProducer APIKeyScopes = "producer" // allows api key access a tool producer
)

func (s *Service) generateToken() (string, error) {
	randomBytes := make([]byte, randomKeyLength/2) // there are 2 hex chars per byte, we can guarantee output of 64 chars this way
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("generate random token bytes: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}
