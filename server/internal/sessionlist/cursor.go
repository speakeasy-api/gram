// Package sessionlist implements stable timestamp/UUID pagination for session feeds.
package sessionlist

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Fingerprint binds a cursor to its tenant, scope, filters, and ordering.
func Fingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// Encode records the ordering key rather than relying on the continued
// existence of the last row. The UUID breaks ties between equal timestamps.
func Encode(at time.Time, id uuid.UUID, fingerprint string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id.String() + "|" + fingerprint))
}

// Decode rejects a cursor reused with a different scope or ordering.
func Decode(raw *string, fingerprint string) (pgtype.Timestamptz, uuid.NullUUID, error) {
	at := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false}
	id := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if raw == nil || *raw == "" {
		return at, id, nil
	}
	// A cursor contains only a timestamp, UUID, and SHA-256 fingerprint.
	const maxCursorLength = 256
	if len(*raw) > maxCursorLength {
		return at, id, fmt.Errorf("invalid session cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(*raw)
	if err != nil {
		return at, id, fmt.Errorf("decode session cursor: %w", err)
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 || parts[2] != fingerprint {
		return at, id, fmt.Errorf("cursor does not match session filters")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return at, id, fmt.Errorf("parse cursor timestamp: %w", err)
	}
	parsedID, err := uuid.Parse(parts[1])
	if err != nil {
		return at, id, fmt.Errorf("parse cursor id: %w", err)
	}
	return pgtype.Timestamptz{Time: timestamp, InfinityModifier: 0, Valid: true}, uuid.NullUUID{UUID: parsedID, Valid: true}, nil
}

// Date parses an optional inclusive or exclusive timestamp boundary.
func Date(value *string) (pgtype.Timestamptz, error) {
	result := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false}
	if value == nil {
		return result, nil
	}
	timestamp, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return result, fmt.Errorf("parse session date: %w", err)
	}
	result.Time, result.Valid = timestamp, true
	return result, nil
}
