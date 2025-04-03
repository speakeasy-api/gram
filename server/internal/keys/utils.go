package keys

import (
	"crypto/rand"
	"encoding/hex"
)

type APIKeyScopes string

const keyPrefix = "gram_"
const randomKeyLength = 64

const (
	APIKeyScopesConsumer APIKeyScopes = "consumer" // allows api key access a tool consumer
	APIKeyScopesProducer APIKeyScopes = "producer" // allows api key access a tool producer
)

func generateKey() (string, error) {
	randomBytes := make([]byte, randomKeyLength/2) // there are 2 hex chars per byte, we can guarantee output of 64 chars this way
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", err
	}
	return keyPrefix + hex.EncodeToString(randomBytes), nil
}
