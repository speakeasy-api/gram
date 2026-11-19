package keys

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// maxListKeysPageSize mirrors the Maximum on keys.list's limit, so a direct
// caller that skips the HTTP validation still gets a bounded page.
const maxListKeysPageSize = 100

// keys.list walks keys by (created_at DESC, id DESC). created_at alone is not
// unique, so a page boundary carries both halves of that tuple.
func encodeKeyListCursor(createdAt time.Time, id uuid.UUID) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// A cursor is client-supplied, so each part is parsed and round-trip checked:
// a tampered value is rejected rather than turned into a different page.
func decodeKeyListCursor(cursor string) (time.Time, uuid.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("decode key list cursor: %w", err)
	}

	timestampText, idText, found := strings.Cut(string(decoded), "|")
	if !found {
		return time.Time{}, uuid.Nil, errors.New("decode key list cursor: invalid format")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, timestampText)
	if err != nil || createdAt.UTC().Format(time.RFC3339Nano) != timestampText {
		return time.Time{}, uuid.Nil, errors.New("decode key list cursor: invalid timestamp")
	}

	id, err := uuid.Parse(idText)
	if err != nil || id == uuid.Nil || id.String() != idText {
		return time.Time{}, uuid.Nil, errors.New("decode key list cursor: invalid id")
	}

	return createdAt, id, nil
}

// keyNameContainsPattern turns a search term into an ILIKE pattern that
// matches it anywhere in a name. LIKE wildcards in the term match literally.
func keyNameContainsPattern(search string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search) + "%"
}
