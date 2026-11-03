package audit

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// securityCursorPrefix marks a cursor issued for the security log. The two
// logs number their rows from different ranges, so a cursor replayed against
// the other log would page silently from the wrong place.
const securityCursorPrefix = "security:"

// EncodeCursor encodes an opaque audit pagination cursor for a category's log.
// Configuration cursors keep the original "seq:id" payload, so cursors clients
// already hold keep working; security cursors carry the category.
func EncodeCursor(category Category, seq int64, id string) string {
	payload := fmt.Sprintf("%d:%s", seq, id)
	if category == CategorySecurity {
		payload = securityCursorPrefix + payload
	}
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// Cursor is a decoded audit pagination cursor.
type Cursor struct {
	Seq int64
	// Category is the log the cursor was issued for.
	Category Category
}

// DecodeCursor extracts the audit sequence, and the log it was issued for,
// from an opaque pagination cursor.
func DecodeCursor(cursor string) (Cursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor: %w", err)
	}

	payload := string(decoded)
	category := CategoryConfiguration
	if rest, ok := strings.CutPrefix(payload, securityCursorPrefix); ok {
		payload, category = rest, CategorySecurity
	}

	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return Cursor{}, fmt.Errorf("invalid cursor format")
	}

	seq, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Cursor{}, fmt.Errorf("parse cursor seq: %w", err)
	}

	return Cursor{Seq: seq, Category: category}, nil
}
