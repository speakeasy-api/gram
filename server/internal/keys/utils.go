package keys

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

type APIKeyScope int

const randomKeyLength = 64

const (
	APIKeyScopeInvalid  APIKeyScope = iota
	APIKeyScopeConsumer APIKeyScope = iota
	APIKeyScopeProducer APIKeyScope = iota
)

var APIKeyScopes = map[string]APIKeyScope{
	"invalid":  APIKeyScopeInvalid,
	"consumer": APIKeyScopeConsumer,
	"producer": APIKeyScopeProducer,
}

func (scope APIKeyScope) String() string {
	switch scope {
	case APIKeyScopeConsumer:
		return "consumer"
	case APIKeyScopeProducer:
		return "producer"
	default:
		return "invalid"
	}
}

func (s *Service) generateToken() (string, error) {
	randomBytes := make([]byte, randomKeyLength/2) // there are 2 hex chars per byte, we can guarantee output of 64 chars this way
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("generate random token bytes: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}
