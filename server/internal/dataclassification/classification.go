// Package dataclassification defines how Speakeasy documents what every stored
// database column contains.
//
// Each column in server/database/schema.sql (Postgres) and
// server/clickhouse/schema.sql (ClickHouse) carries a comment whose last
// non-empty line is a data class marker:
//
//	COMMENT ON COLUMN chat_messages.content IS 'Raw message text.
//	@access: opaque-restricted';
//
// Class names follow the levels of Speakeasy's data classification policy
// (public, internal use, confidential, restricted). A suffix or prefix narrows
// a level: confidential-pii and opaque-confidential are kinds of confidential
// data, and secret-restricted and opaque-restricted are kinds of restricted
// data.
//
// Descriptive prose may precede the marker. The class records what the data
// is. Each consumer, such as a read-only SQL agent, log redaction, retention
// or exports, decides which classes it accepts. Tests in this package fail
// when any column is missing a valid marker.
package dataclassification

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Class is the data class of an entire column.
type Class string

const (
	// Confidential is reviewed operational metadata: internal IDs, foreign
	// keys, timestamps, Speakeasy-controlled enums and flags, counts and metrics,
	// short labels and slugs.
	Confidential Class = "confidential"
	// ConfidentialPII identifies or contacts a natural person: emails,
	// personal names, handles, avatar URLs, IP addresses, user agents, person
	// IDs in external identity providers.
	ConfidentialPII Class = "confidential-pii"
	// OpaqueConfidential is a short identifier or label whose value customers,
	// their systems or producers choose, so Speakeasy can't vouch for it, but
	// which support needs to see: names of tools, skills, agents, MCP servers
	// and clients, span and event names, producer-supplied session IDs,
	// attribute keys. Free text and payloads are OpaqueRestricted instead.
	OpaqueConfidential Class = "opaque-confidential"
	// SecretRestricted grants access or proves identity: passwords and their
	// hashes, API keys and tokens and their hashes, client secrets, signing
	// keys, encrypted credentials, auth-flow state and verifiers.
	SecretRestricted Class = "secret-restricted"
	// Restricted is known sensitive data that is neither a credential nor
	// opaque content, such as billing-system identifiers or security
	// findings, and anything whose nature is uncertain.
	Restricted Class = "restricted"
	// OpaqueRestricted is arbitrary content supplied by customers, their users,
	// their systems or models: messages, prompts, tool arguments and results,
	// request and response data, telemetry attributes, uploaded documents,
	// free-form descriptions and customer-supplied JSON. Its contents are not
	// controlled by Speakeasy, so it is treated as possibly containing restricted
	// data.
	OpaqueRestricted Class = "opaque-restricted"
)

// Classes lists every valid class.
var Classes = []Class{Confidential, ConfidentialPII, OpaqueConfidential, SecretRestricted, Restricted, OpaqueRestricted}

// Marker prefixes the classification line of a column comment.
const Marker = "@access:"

// Valid reports whether c is one of Classes.
func Valid(c Class) bool {
	return slices.Contains(Classes, c)
}

// Parse extracts the class from a column comment. The comment must contain
// exactly one marker line, it must be the last non-empty line, and its value
// must be a valid class. Any other shape is an error, so a stray or duplicated
// marker can never be silently ignored.
func Parse(comment string) (Class, error) {
	lines := strings.Split(strings.ReplaceAll(comment, "\r\n", "\n"), "\n")
	markers, index, last := 0, -1, -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			last = i
		}
		if strings.Contains(line, Marker) {
			markers++
			index = i
		}
	}
	switch {
	case markers == 0:
		return "", errors.New("missing " + Marker + " marker")
	case markers > 1:
		return "", errors.New("more than one " + Marker + " marker")
	case index != last:
		return "", errors.New(Marker + " marker must be the last non-empty line")
	case !strings.HasPrefix(strings.TrimSpace(lines[index]), Marker):
		return "", errors.New(Marker + " marker must start its line")
	}
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[index]), Marker))
	class := Class(value)
	if !Valid(class) {
		return "", fmt.Errorf("unknown class %q", value)
	}
	return class, nil
}
