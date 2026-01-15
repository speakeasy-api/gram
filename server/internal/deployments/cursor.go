package deployments

import (
	"encoding/base64"
	"fmt"
	"strconv"
)

// encodeCursor encodes a seq value as a URL-safe base64 cursor.
func encodeCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(seq, 10)))
}

// decodeCursor decodes a URL-safe base64 cursor to a seq value.
func decodeCursor(cursor string) (int64, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("decode cursor: %w", err)
	}
	seq, err := strconv.ParseInt(string(decoded), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cursor seq: %w", err)
	}
	return seq, nil
}
