package anthropicinference

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const (
	// messageIdentityPrefix namespaces stored inference message identities.
	messageIdentityPrefix = "anthropic-inference:"
	// alignmentWindow bounds how much stored history is loaded to align an
	// incoming transcript. A frame only ever overlaps the newest stored
	// messages, so the window has to cover one delivery's worth of history,
	// not the whole conversation.
	alignmentWindow = 512
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
// message: the input the model is about to act on. Everything before it was
// delivered, and judged, on an earlier frame.
func currentTurnStart(messages []Message) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "assistant" {
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
// stored is oldest first; frame holds the content hash of each incoming
// message in order.
//
// The newest stored message is located in the frame by matching the longest
// run of stored history that ends on it. Preferring the longest run, and the
// earliest position among equal runs, tells a repeated identical message
// ("continue", "continue", "continue") apart from a redelivery of the same
// three. Client-side rewrites of older history — a compaction summary in
// place of early turns, an edited or truncated block — fall outside the run
// and are neither stored again nor scanned again.
//
// When the newest stored message itself was rewritten, the next few older
// ones are tried as the anchor instead. The result is the index of the first
// incoming message that is not stored and the chain hash its identity
// continues from. ok is false when no anchor is present in the frame.
func alignTranscript(stored []messageIdentity, frame [][]byte) (start int, prev []byte, ok bool) {
	for anchor := 0; anchor < alignmentAnchors && anchor < len(stored); anchor++ {
		history := stored[:len(stored)-anchor]
		newest := history[len(history)-1]
		if newest.content == nil {
			continue
		}
		bestRun, bestPos := 0, -1
		for pos := len(frame) - 1; pos >= 0; pos-- {
			run := 0
			for run < len(history) && pos-run >= 0 && bytes.Equal(frame[pos-run], history[len(history)-1-run].content) {
				run++
			}
			if run > 0 && run >= bestRun {
				bestRun, bestPos = run, pos
			}
		}
		if bestPos >= 0 {
			return bestPos + 1, newest.chain, true
		}
	}
	return 0, nil, false
}
