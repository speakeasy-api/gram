package agentmanagement

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Agents are listed by (LOWER(name), id), so a page boundary needs both halves
// of that tuple: names are not unique, and the id alone does not say where in
// the ordering the page stopped.
func encodeAgentCursor(name string, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.ToLower(name) + "|" + id.String()))
}

// A cursor is client-supplied, so every part is re-validated and round-trip
// checked rather than coerced: a tampered value is rejected, not silently
// turned into a different page.
func decodeAgentCursor(cursor string) (string, uuid.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("decode agent cursor: %w", err)
	}

	payload := string(decoded)
	// A name may contain "|", so the id is taken from the last one.
	separator := strings.LastIndex(payload, "|")
	if separator < 0 {
		return "", uuid.Nil, errors.New("decode agent cursor: invalid format")
	}
	name, idText := payload[:separator], payload[separator+1:]
	if name == "" {
		return "", uuid.Nil, errors.New("decode agent cursor: empty name")
	}
	if strings.ToLower(name) != name {
		return "", uuid.Nil, errors.New("decode agent cursor: name not normalized")
	}

	id, err := uuid.Parse(idText)
	if err != nil || id == uuid.Nil || id.String() != idText {
		return "", uuid.Nil, errors.New("decode agent cursor: invalid id")
	}

	return name, id, nil
}
