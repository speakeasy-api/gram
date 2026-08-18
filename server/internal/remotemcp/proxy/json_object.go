package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// object preserves unmodeled members of JSON objects the proxy mutates.
type object map[string]json.RawMessage

// decodeObject treats an empty payload as an empty object and rejects null so the
// returned map is always writable.
func decodeObject(payload json.RawMessage) (object, error) {
	members := object{}
	if len(payload) == 0 {
		return members, nil
	}

	if err := json.Unmarshal(payload, &members); err != nil {
		return nil, fmt.Errorf("decode payload as a JSON object: %w", err)
	}
	if members == nil {
		return nil, errors.New("payload is null, not a JSON object")
	}

	return members, nil
}

func (o object) encode() (json.RawMessage, error) {
	encoded, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("encode JSON object: %w", err)
	}

	return encoded, nil
}

const (
	cacheScopeMember = "cacheScope"
	ttlMsMember      = "ttlMs"

	cacheScopePrivate = `"private"`

	ttlMsStale = `0`
)

// confineToCaller prevents caller-specific results from being shared or retained
// after authorization changes. It rewrites only caching hints the upstream sent.
func (o object) confineToCaller() {
	_, hasScope := o[cacheScopeMember]
	_, hasTTL := o[ttlMsMember]

	// Remove aliases that case-folding parsers could prefer over the confined values.
	for name := range o {
		switch {
		case name == cacheScopeMember || name == ttlMsMember:
		case strings.EqualFold(name, cacheScopeMember):
			hasScope = true
			delete(o, name)
		case strings.EqualFold(name, ttlMsMember):
			hasTTL = true
			delete(o, name)
		}
	}

	if !hasScope && !hasTTL {
		return
	}

	o[cacheScopeMember] = json.RawMessage(cacheScopePrivate)
	o[ttlMsMember] = json.RawMessage(ttlMsStale)
}
