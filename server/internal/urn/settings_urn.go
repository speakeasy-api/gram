package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

// The organization-scoped settings URN types (ChatAnalysisSettings,
// SkillEfficacySettings) are identical modulo their prefix and Go type name.
// These helpers hold the shared implementation; the typed files keep thin
// wrappers so callers stay compile-time safe against mixing subjects.

func settingsURNParse(prefix, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return "", fmt.Errorf("%w: expected two segments (%s:<organization_id>)", ErrInvalid, prefix)
	}
	if parts[0] != prefix {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return "", fmt.Errorf("%w: expected %s urn (got: %q)", ErrInvalid, prefix, truncated)
	}

	if err := settingsURNValidate(parts[1]); err != nil {
		return "", err
	}
	return parts[1], nil
}

func settingsURNValidate(id string) error {
	switch {
	case id == "":
		return fmt.Errorf("%w: empty id", ErrInvalid)
	case len(id) > maxSegmentLength:
		return fmt.Errorf("%w: id segment is too long (max %d, got %d)", ErrInvalid, maxSegmentLength, len(id))
	case strings.Contains(id, delimiter):
		return fmt.Errorf("%w: id contains delimiter", ErrInvalid)
	default:
		return nil
	}
}

func settingsURNString(prefix, id string) string {
	return prefix + delimiter + id
}

func settingsURNMarshalJSON(prefix, id string) ([]byte, error) {
	if err := settingsURNValidate(id); err != nil {
		return nil, err
	}

	b, err := json.Marshal(settingsURNString(prefix, id))
	if err != nil {
		return nil, fmt.Errorf("%s urn to json: %w", prefix, err)
	}
	return b, nil
}

func settingsURNUnmarshalJSON(prefix string, data []byte) (string, error) {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return "", fmt.Errorf("read %s urn string from json: %w", prefix, err)
	}

	id, err := settingsURNParse(prefix, value)
	if err != nil {
		return "", fmt.Errorf("parse %s urn json string: %w", prefix, err)
	}
	return id, nil
}

// settingsURNScan backs the sql.Scanner implementations; callers handle the
// nil-value no-op before delegating here.
func settingsURNScan(prefix, typeName string, value any) (string, error) {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return "", fmt.Errorf("cannot scan %T into %s", value, typeName)
	}

	id, err := settingsURNParse(prefix, text)
	if err != nil {
		return "", fmt.Errorf("scan database value: %w", err)
	}
	return id, nil
}

func settingsURNValue(prefix, id string) (driver.Value, error) {
	if err := settingsURNValidate(id); err != nil {
		return nil, err
	}
	return settingsURNString(prefix, id), nil
}

func settingsURNMarshalText(prefix, id string) ([]byte, error) {
	if err := settingsURNValidate(id); err != nil {
		return nil, fmt.Errorf("marshal %s urn text: %w", prefix, err)
	}
	return []byte(settingsURNString(prefix, id)), nil
}

func settingsURNUnmarshalText(prefix string, text []byte) (string, error) {
	id, err := settingsURNParse(prefix, string(text))
	if err != nil {
		return "", fmt.Errorf("unmarshal %s urn text: %w", prefix, err)
	}
	return id, nil
}
