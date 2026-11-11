package audit

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrPageTooLarge reports that a serialized audit payload exceeds its byte bound.
var ErrPageTooLarge = errors.New("audit page exceeds byte limit")

// MarshalBoundedPage serializes a payload, not its transport envelope, and
// returns no bytes if encoding fails or the complete payload exceeds maxBytes.
func MarshalBoundedPage(value any, maxBytes int) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal audit page: %w", err)
	}
	if len(encoded) > maxBytes {
		return nil, ErrPageTooLarge
	}
	return encoded, nil
}
