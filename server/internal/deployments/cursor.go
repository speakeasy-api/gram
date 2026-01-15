package deployments

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// encodeCursor encodes a seq value and UUID as a URL-safe base64 cursor.
// Format: base64("seq:uuid")
func encodeCursor(seq int64, id uuid.UUID) string {
	payload := fmt.Sprintf("%d:%s", seq, id.String())
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// decodeCursor decodes a URL-safe base64 cursor to a seq value and UUID.
func decodeCursor(cursor string) (int64, uuid.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, uuid.Nil, fmt.Errorf("decode cursor: %w", err)
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return 0, uuid.Nil, fmt.Errorf("invalid cursor format")
	}

	seq, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, uuid.Nil, fmt.Errorf("parse cursor seq: %w", err)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return 0, uuid.Nil, fmt.Errorf("parse cursor id: %w", err)
	}

	return seq, id, nil
}
