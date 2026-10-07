package storage

import (
	"encoding/json"
	"fmt"
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
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		return nil, fmt.Errorf("decode storage bucket mapping: %w", err)
	}
	if mapping == nil {
		return nil, fmt.Errorf("storage bucket mapping must be a JSON object")
	}
	physical := map[string]string{}
	for logical, bucket := range mapping {
		if !logicalBucket.MatchString(logical) || !physicalBucket.MatchString(bucket) || strings.HasPrefix(bucket, "goog") {
			return nil, fmt.Errorf("invalid storage bucket mapping for logical name %q", logical)
		}
		if prior, ok := physical[bucket]; ok {
			return nil, fmt.Errorf("logical buckets %q and %q resolve to the same physical bucket", prior, logical)
		}
		physical[bucket] = logical
	}
	return mapping, nil
}
