package gateway

import (
	"encoding/json"
	"fmt"
)

type Nullable[T comparable] struct {
	Value T
	Valid bool
}

func (n *Nullable[T]) IsValid() bool {
	return n != nil && n.Valid
}

func (src Nullable[T]) MarshalJSON() ([]byte, error) {
	if !src.Valid {
		return []byte("null"), nil
	}

	bs, err := json.Marshal(src.Value)
	if err != nil {
		return nil, fmt.Errorf("marshal nullable: %w", err)
	}

	return bs, nil
}

func (dst *Nullable[T]) UnmarshalJSON(b []byte) error {
	var s *T
	err := json.Unmarshal(b, &s)
	if err != nil {
		return fmt.Errorf("unmarshal nullable: %w", err)
	}

	if s == nil {
		var zero Nullable[T]
		*dst = zero
	} else {
		*dst = Nullable[T]{Value: *s, Valid: true}
	}

	return nil
}

type NullString = Nullable[string]
