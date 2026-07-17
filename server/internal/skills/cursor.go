package skills

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func encodeSkillCursor(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

func decodeSkillCursor(cursor string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("decode skill cursor: %w", err)
	}
	if len(decoded) == 0 {
		return "", errors.New("decode skill cursor: empty name")
	}

	name := string(decoded)
	normalized, err := normalizeSkillName(name)
	if err != nil || normalized != name {
		return "", errors.New("decode skill cursor: invalid normalized name")
	}

	return name, nil
}

func encodeSkillVersionCursor(createdAt time.Time, id uuid.UUID) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeSkillVersionCursor(cursor string) (time.Time, uuid.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("decode skill version cursor: %w", err)
	}

	payload := string(decoded)
	if strings.Count(payload, "|") != 1 {
		return time.Time{}, uuid.Nil, errors.New("decode skill version cursor: invalid format")
	}
	timestampText, idText, _ := strings.Cut(payload, "|")

	createdAt, err := time.Parse(time.RFC3339Nano, timestampText)
	if err != nil || createdAt.UTC().Format(time.RFC3339Nano) != timestampText {
		return time.Time{}, uuid.Nil, errors.New("decode skill version cursor: invalid timestamp")
	}

	id, err := uuid.Parse(idText)
	if err != nil || id == uuid.Nil || id.String() != idText {
		return time.Time{}, uuid.Nil, errors.New("decode skill version cursor: invalid id")
	}

	return createdAt, id, nil
}
