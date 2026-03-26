package pylon

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"log/slog"
	"sync"
)

type Pylon struct {
	logger   *slog.Logger
	enabled  bool
	hashPool *sync.Pool
}

func NewPylon(logger *slog.Logger, secret string) (*Pylon, error) {
	if secret == "" {
		return &Pylon{logger: logger, enabled: false, hashPool: nil}, nil
	}

	secretBytes, err := hex.DecodeString(secret)
	if err != nil {
		return nil, errors.New("unable to decode pylon identity secret")
	}

	return &Pylon{logger: logger, enabled: true, hashPool: &sync.Pool{
		New: func() any {
			return hmac.New(sha256.New, secretBytes)
		},
	}}, nil
}

func (p *Pylon) Sign(email string) (string, error) {
	if !p.enabled {
		return "", nil
	}

	// Retrieve an HMAC hash object from the pool
	h, ok := p.hashPool.Get().(hash.Hash)
	if !ok {
		return "", errors.New("unable to decode pylon identity secret")
	}
	defer p.hashPool.Put(h)

	// Reset the hash to prepare it for a new computation
	h.Reset()
	h.Write([]byte(email))

	// Compute the signature
	signature := h.Sum(nil)

	// Encode the signature to a hex string and return it
	return hex.EncodeToString(signature), nil
}
