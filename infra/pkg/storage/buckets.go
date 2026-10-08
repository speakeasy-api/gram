package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var logicalBucket = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// ParseBucketMapping reads GRAM_STORAGE_BUCKETS, the JSON mapping produced by the
// deployment chart. Empty configuration is allowed; installing a runner whose
// bucket is missing fails at startup. No name is guessed or created at runtime.
func ParseBucketMapping(raw string) (map[string]string, error) {
	mapping := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return mapping, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode storage bucket mapping: %w", err)
	}
	if opening != json.Delim('{') {
		return nil, fmt.Errorf("storage bucket mapping must be a JSON object")
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decode storage bucket key: %w", err)
		}
		logical, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("storage bucket key must be a string")
		}
		if _, exists := mapping[logical]; exists {
			return nil, fmt.Errorf("duplicate logical bucket %q", logical)
		}
		var bucket string
		if err := decoder.Decode(&bucket); err != nil {
			return nil, fmt.Errorf("decode physical bucket: %w", err)
		}
		mapping[logical] = bucket
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("close storage bucket mapping: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after storage bucket mapping")
	}
	physical := map[string]string{}
	for logical, bucket := range mapping {
		if !logicalBucket.MatchString(logical) || !validPhysicalBucket(bucket) {
			return nil, fmt.Errorf("invalid storage bucket mapping for logical name %q", logical)
		}
		if prior, ok := physical[bucket]; ok {
			return nil, fmt.Errorf("logical buckets %q and %q resolve to the same physical bucket", prior, logical)
		}
		physical[bucket] = logical
	}
	return mapping, nil
}

// validPhysicalBucket matches the deployment's portable GCS naming rules,
// including the reserved brand and its zero-substitution spellings.
func validPhysicalBucket(bucket string) bool {
	return physicalBucket.MatchString(bucket) && !strings.HasPrefix(bucket, "goog") &&
		!strings.Contains(strings.ReplaceAll(bucket, "0", "o"), "google")
}
