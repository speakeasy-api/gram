package keys

import (
	"crypto/rand"
	"encoding/hex"
)

type APIKeyScopes string

const keyPrefix = "gram_"
const randomKeyLength = 64

const (
	APIKeyScopesReadConsumer  APIKeyScopes = "read:consumer"  // allows read access as a tool consumer
	APIKeyScopesWriteConsumer APIKeyScopes = "write:consumer" // allows write access (execution) as a tool consumer
)

func generateKey() (string, error) {
	randomBytes := make([]byte, randomKeyLength/2) // there are 2 hex chars per byte, we can guarantee output of 64 chars this way
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", err
	}
	return keyPrefix + hex.EncodeToString(randomBytes), nil
}
