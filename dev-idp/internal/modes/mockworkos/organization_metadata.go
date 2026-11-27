package mockworkos

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"unicode"
)

const (
	// maxMetadataKeyLength is the longest organization metadata key WorkOS
	// accepts.
	maxMetadataKeyLength = 40

	// maxMetadataValueLength is the longest organization metadata value WorkOS
	// accepts.
	maxMetadataValueLength = 600
)

var errMetadataNotStringObject = errors.New("metadata must be an object of string values")

// decodeCreateMetadata validates the metadata of an organization create and
// returns the JSON object to store. Absent or null metadata returns an invalid
// NullString, which stores the empty object.
func decodeCreateMetadata(raw json.RawMessage) (sql.NullString, error) {
	if metadataAbsent(raw) {
		return sql.NullString{String: "", Valid: false}, nil
	}

	// Decoded with pointer values so a null value is rejected rather than
	// silently stored as an empty string.
	var metadata map[string]*string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return sql.NullString{String: "", Valid: false}, errMetadataNotStringObject
	}

	for key, value := range metadata {
		if value == nil {
			return sql.NullString{String: "", Valid: false}, errMetadataNotStringObject
		}
		if err := validateMetadataEntry(key, value); err != nil {
			return sql.NullString{String: "", Valid: false}, err
		}
	}

	encoded, err := json.Marshal(metadata)
	if err != nil {
		return sql.NullString{String: "", Valid: false}, fmt.Errorf("encode metadata: %w", err)
	}

	return sql.NullString{String: string(encoded), Valid: true}, nil
}

// decodeMetadataPatch validates the metadata of an organization update and
// returns the merge patch UpdateOrganization applies. Absent or null metadata
// returns an invalid NullString, which leaves metadata alone. An empty object
// is returned as exactly "{}", which deletes every key.
func decodeMetadataPatch(raw json.RawMessage) (sql.NullString, error) {
	if metadataAbsent(raw) {
		return sql.NullString{String: "", Valid: false}, nil
	}

	var patch map[string]*string
	if err := json.Unmarshal(raw, &patch); err != nil {
		return sql.NullString{String: "", Valid: false}, errMetadataNotStringObject
	}

	for key, value := range patch {
		if err := validateMetadataEntry(key, value); err != nil {
			return sql.NullString{String: "", Valid: false}, err
		}
	}

	// Re-encoding normalizes the patch, so an empty object is always "{}".
	encoded, err := json.Marshal(patch)
	if err != nil {
		return sql.NullString{String: "", Valid: false}, fmt.Errorf("encode metadata: %w", err)
	}

	return sql.NullString{String: string(encoded), Valid: true}, nil
}

func metadataAbsent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// validateMetadataEntry applies the WorkOS limits to one metadata entry. A nil
// value is a deletion and only the key is checked.
func validateMetadataEntry(key string, value *string) error {
	if key == "" || len(key) > maxMetadataKeyLength || !isASCII(key) {
		return fmt.Errorf("metadata key %q must be 1 to %d ASCII characters", key, maxMetadataKeyLength)
	}
	if value != nil && (len(*value) > maxMetadataValueLength || !isASCII(*value)) {
		return fmt.Errorf("metadata value for %q must be at most %d ASCII characters", key, maxMetadataValueLength)
	}

	return nil
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}

	return true
}
