package anthropicinference

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
)

const (
	// messageIdentityPrefix namespaces stored inference message identities.
	messageIdentityPrefix = "anthropic-inference:"
	// alignmentAnchors bounds how many of the newest stored messages are tried
	// as the alignment anchor. The newest one is missing from a frame only when
	// the client rewrote it in place, and a rewrite that reaches this far back
	// is a new transcript for our purposes.
	alignmentAnchors = 8
)

// messageIdentity is what the store remembers about one message-level row.
type messageIdentity struct {
	// content hashes the role and canonical content, and is what an incoming
	// message is matched on.
	content []byte
	// chain hashes the content together with the predecessor's chain, and is
	// the stored identity: two identical messages at different positions stay
	// distinct, and a redelivery of the same history maps to the same rows.
	chain []byte
}

// conversationMessages keeps the roles that make up the stored conversation
// and the policy inputs. Other roles are protocol scaffolding.
func conversationMessages(messages []Message) []Message {
	kept := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "user" || msg.Role == "assistant" {
			kept = append(kept, msg)
		}
	}
	return kept
}

// currentTurnStart is the index of the first message after the last assistant
// message: the input the model is about to act on. Earlier content is not
// necessarily accepted; only an accepted checkpoint can establish that.
func currentTurnStart(messages []Message) int {
	for index, message := range slices.Backward(messages) {
		if message.Role == "assistant" {
			return index + 1
		}
	}
	return 0
}

func contentHash(msg Message) []byte {
	h := sha256.New()
	h.Write([]byte(msg.Role))
	h.Write([]byte{0})
	// Deliveries may re-encode the same content with different whitespace.
	var canonical bytes.Buffer
	if err := json.Compact(&canonical, msg.Content); err != nil {
		h.Write(msg.Content)
	} else {
		h.Write(canonical.Bytes())
	}
	return h.Sum(nil)
}

func chainHash(prev, content []byte) []byte {
	h := sha256.New()
	h.Write(prev)
	h.Write(content)
	return h.Sum(nil)
}

func messageIdentityID(chain []byte) string {
	return messageIdentityPrefix + hex.EncodeToString(chain)
}

// parseMessageIdentity reads a stored row back into its identity. A row from
// before content hashing carries an ordinal instead of a chain and can never
// match an incoming message; it is reported with a nil content hash.
func parseMessageIdentity(externalID string, content []byte) messageIdentity {
	if len(content) == 0 {
		return messageIdentity{content: nil, chain: nil}
	}
	chain, err := hex.DecodeString(strings.TrimPrefix(externalID, messageIdentityPrefix))
	if err != nil {
		return messageIdentity{content: nil, chain: nil}
	}
	return messageIdentity{content: content, chain: chain}
}

// alignTranscript finds where an incoming transcript continues stored history.
// stored is oldest first. Try the newest stored anchor first, scanning the
// incoming frame backward and returning immediately on the first content match.
// Older history is neither hashed nor compared after a match.
//
// This archival heuristic deliberately treats a new message identical to an
// anchor as already stored. Rewritten newest messages can fall back to the next
// few stored anchors. Enforcement uses acceptedPrefix, never this heuristic.
func alignTranscript(stored []messageIdentity, frame []Message) (start int, prev []byte, ok bool) {
	// Cache hashes lazily so fallback anchors do not rehash the same messages.
	hashes := make([][]byte, len(frame))
	for anchor := 0; anchor < alignmentAnchors && anchor < len(stored); anchor++ {
		newest := stored[len(stored)-1-anchor]
		if newest.content == nil {
			continue
		}
		for pos, msg := range slices.Backward(frame) {
			if hashes[pos] == nil {
				hashes[pos] = contentHash(msg)
			}
			if bytes.Equal(newest.content, hashes[pos]) {
				return pos + 1, newest.chain, true
			}
		}
	}
	return 0, nil, false
}
