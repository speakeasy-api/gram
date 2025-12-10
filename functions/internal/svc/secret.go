package svc

import (
	"encoding/json"
	"fmt"
)

type Secret[T any] struct {
	value T
}

func NewSecret[T any](v T) Secret[T] {
	return Secret[T]{value: v}
}

func (s Secret[T]) Reveal() T {
	return s.value
}

// String implements fmt.Stringer to prevent secrets from leaking in logs
func (s Secret[T]) String() string {
	return "[REDACTED]"
}

// GoString implements fmt.GoStringer to prevent secrets from leaking in debug output
func (s Secret[T]) GoString() string {
	return "Secret{[REDACTED]}"
}

// MarshalJSON implements json.Marshaler to prevent secrets from leaking in JSON
func (s Secret[T]) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal("[REDACTED]")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal secret: %w", err)
	}
	return data, nil
}

// MarshalText implements encoding.TextMarshaler to prevent secrets from leaking in text encoding
func (s Secret[T]) MarshalText() ([]byte, error) {
	return []byte("[REDACTED]"), nil
}
