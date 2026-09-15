package audit

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// EncodeCursor preserves the opaque audit pagination cursor wire format.
func EncodeCursor(seq int64, id string) string {
	payload := fmt.Sprintf("%d:%s", seq, id)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// DecodeCursor extracts the audit sequence from an opaque pagination cursor.
func DecodeCursor(cursor string) (int64, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("decode cursor: %w", err)
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid cursor format")
	}

	seq, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cursor seq: %w", err)
	}

	return seq, nil
}
