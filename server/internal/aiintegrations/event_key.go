package aiintegrations

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// eventKey is the ordered identity of an ingested analytics row. Its hash is
// stored on every row (cursor.event_hash, claude_chat.event_hash) so
// consumers needing exact-once sums can dedupe by
// (gram_project_id, <provider>.event_hash) — covering the crash window
// between a ClickHouse write and the watermark advance, where one window can
// be re-ingested.
//
// The hash is a contract with already-ingested data. Encoding (v1, frozen):
// fields are rendered in order, joined with '|', and SHA-256'd. Adding,
// removing, or reordering a key's fields changes the hash of the same
// logical event and orphans every hash already in ClickHouse, so treat each
// provider's key as frozen once it ships — golden tests in
// event_key_test.go enforce this.
//
// Rules for new keys:
//   - Lead with a kind literal (e.g. "usage", "cost") so identical tuples
//     from different report kinds cannot collide.
//   - Normalize free-form fields before appending (e.g.
//     conv.NormalizeEmail) so equal logical values hash equally.
//
// Known v1 limitation, accepted: a field containing '|' could in theory
// re-segment into a neighbor (only email local parts can carry one here,
// and field counts are fixed per key). Cursor's key predates this type and
// is live in production, so the encoding cannot change retroactively.
type eventKey []any

func (k eventKey) hash() string {
	fields := make([]string, len(k))
	for i, part := range k {
		switch v := part.(type) {
		case string:
			fields[i] = v
		case int64:
			fields[i] = strconv.FormatInt(v, 10)
		case float64:
			fields[i] = strconv.FormatFloat(v, 'f', -1, 64)
		case time.Time:
			fields[i] = strconv.FormatInt(v.UTC().UnixMilli(), 10)
		default:
			// Programmer error: a new field type needs an explicit, stable
			// rendering rule here. Golden tests exercise every key.
			panic(fmt.Sprintf("eventKey: no stable rendering for field %d (%T)", i, part))
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return hex.EncodeToString(sum[:])
}
