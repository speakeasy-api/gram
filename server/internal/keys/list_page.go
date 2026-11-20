package keys

import (
	"crypto/sha256"
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
// unique, so a page boundary carries both halves of that tuple. It also
// carries a digest of the organization and search that produced it: a
// position is only meaningful inside that result set, so a cursor reused with
// a different search or organization is rejected instead of silently
// skipping keys.
func encodeKeyListCursor(createdAt time.Time, id uuid.UUID, scope string) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String() + "|" + scope
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// keyListCursorScope names one result set: an organization's keys, filtered
// by the trimmed search term ("" for no search). Only a digest goes in the
// cursor, so a long search term does not make the cursor long.
func keyListCursorScope(organizationID string, search string) string {
	sum := sha256.Sum256([]byte(organizationID + "\x00" + search))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

// A cursor is client-supplied, so each part is parsed and round-trip checked:
// a tampered value is rejected rather than turned into a different page.
func decodeKeyListCursor(cursor string, scope string) (time.Time, uuid.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("decode key list cursor: %w", err)
	}

	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 {
		return time.Time{}, uuid.Nil, errors.New("decode key list cursor: invalid format")
	}
	timestampText, idText, cursorScope := parts[0], parts[1], parts[2]

	createdAt, err := time.Parse(time.RFC3339Nano, timestampText)
	if err != nil || createdAt.UTC().Format(time.RFC3339Nano) != timestampText {
		return time.Time{}, uuid.Nil, errors.New("decode key list cursor: invalid timestamp")
	}

	id, err := uuid.Parse(idText)
	if err != nil || id == uuid.Nil || id.String() != idText {
		return time.Time{}, uuid.Nil, errors.New("decode key list cursor: invalid id")
	}

	if cursorScope != scope {
		return time.Time{}, uuid.Nil, errors.New("decode key list cursor: cursor belongs to a different search or organization")
	}

	return createdAt, id, nil
}

// keyNameContainsPattern turns a search term into an ILIKE pattern that
// matches it anywhere in a name. LIKE wildcards in the term match literally.
func keyNameContainsPattern(search string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search) + "%"
}
