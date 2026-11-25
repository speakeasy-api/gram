package mcpfeedback

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// encodeFeedbackCursor names the last report of a page, so the next page
// starts right after it in the newest-first order.
func encodeFeedbackCursor(createdAt time.Time, id uuid.UUID) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// decodeFeedbackCursor reads a cursor encodeFeedbackCursor produced. It
// accepts only the exact form it writes, so a hand-edited cursor is refused
// rather than paging from somewhere unexpected.
func decodeFeedbackCursor(cursor string) (time.Time, uuid.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("decode feedback cursor: %w", err)
	}

	timestampText, idText, found := strings.Cut(string(decoded), "|")
	if !found || strings.Contains(idText, "|") {
		return time.Time{}, uuid.Nil, errors.New("decode feedback cursor: invalid format")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, timestampText)
	if err != nil || createdAt.UTC().Format(time.RFC3339Nano) != timestampText {
		return time.Time{}, uuid.Nil, errors.New("decode feedback cursor: invalid timestamp")
	}

	id, err := uuid.Parse(idText)
	if err != nil || id == uuid.Nil || id.String() != idText {
		return time.Time{}, uuid.Nil, errors.New("decode feedback cursor: invalid id")
	}

	return createdAt, id, nil
}
