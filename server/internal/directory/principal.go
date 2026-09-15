package directory

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	attributePrincipalPrefix = "directory_attribute"
	groupPrincipalPrefix     = "directory_group"
)

// ErrParsingPrincipal indicates that a directory principal is malformed.
var ErrParsingPrincipal = errors.New("parse directory principal")

// GroupPrincipal returns the canonical principal for a directory group.
func GroupPrincipal(id uuid.UUID) string {
	return fmt.Sprintf("%s:%s", groupPrincipalPrefix, id)
}

// AttributePrincipal returns the canonical principal for a directory attribute
// value. The key and value are encoded so they cannot conflict with the
// principal's separators.
func AttributePrincipal(key, value string) string {
	return fmt.Sprintf(
		"%s:%s:%s",
		attributePrincipalPrefix,
		base64.RawURLEncoding.EncodeToString([]byte(key)),
		base64.RawURLEncoding.EncodeToString([]byte(value)),
	)
}

// IsGroupPrincipal reports whether value uses the directory group principal
// prefix. It does not validate the group identifier.
func IsGroupPrincipal(value string) bool {
	return strings.HasPrefix(value, groupPrincipalPrefix+":")
}

// IsAttributePrincipal reports whether value uses the directory attribute
// principal prefix. It does not validate the encoded key and value.
func IsAttributePrincipal(value string) bool {
	return strings.HasPrefix(value, attributePrincipalPrefix+":")
}

// ParseGroupPrincipal parses a directory group principal.
func ParseGroupPrincipal(principal string) (uuid.UUID, error) {
	value, ok := strings.CutPrefix(principal, groupPrincipalPrefix+":")
	if !ok {
		return uuid.Nil, fmt.Errorf("%w: expected directory group prefix", ErrParsingPrincipal)
	}

	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: group id: %w", ErrParsingPrincipal, err)
	}

	return id, nil
}

// ParseAttributePrincipal parses a directory attribute principal.
func ParseAttributePrincipal(principal string) (AttributeValue, error) {
	encoded, ok := strings.CutPrefix(principal, attributePrincipalPrefix+":")
	if !ok {
		return AttributeValue{}, fmt.Errorf("%w: expected directory attribute prefix", ErrParsingPrincipal)
	}

	key, value, ok := strings.Cut(encoded, ":")
	if !ok {
		return AttributeValue{}, fmt.Errorf("%w: missing attribute value", ErrParsingPrincipal)
	}

	decodedKey, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		return AttributeValue{}, fmt.Errorf("%w: attribute key: %w", ErrParsingPrincipal, err)
	}

	decodedValue, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return AttributeValue{}, fmt.Errorf("%w: attribute value: %w", ErrParsingPrincipal, err)
	}

	return AttributeValue{Key: string(decodedKey), Value: string(decodedValue)}, nil
}
