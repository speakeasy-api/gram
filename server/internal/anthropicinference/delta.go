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
	// alignmentWindow is the minimum archive alignment window. Load at least
	// one full incoming frame as well, so repeated frames longer than this
	// window cannot be mistaken for an append on identical redelivery.
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
// stored is oldest first; frame holds the content hash of each incoming
// message in order.
//
// The newest stored message is located in the frame by matching the longest
// run of stored history that ends on it. Preferring the longest run, and the
// earliest position among equal runs, tells a repeated identical message
// ("continue", "continue", "continue") apart from a redelivery of the same
// three. Client-side rewrites of older history — a compaction summary in
// place of early turns, an edited or truncated block — fall outside the run
// and are not stored again. Enforcement uses acceptedPrefix, never this
// archival heuristic.
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
		// Reverse both sequences: a suffix ending at any frame position is
		// now a prefix match. Z matching keeps large repeated frames linear.
		sequence := make([][]byte, 0, len(history)+1+len(frame))
		for _, h := range slices.Backward(history) {
			sequence = append(sequence, h.content)
		}
		sequence = append(sequence, nil) // separator, never a content hash
		for _, f := range slices.Backward(frame) {
			sequence = append(sequence, f)
		}
		matches := prefixMatches(sequence)
		bestRun, bestPos := 0, -1
		for pos := range slices.Backward(frame) {
			run := matches[len(history)+1+len(frame)-1-pos]
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

// prefixMatches computes the Z array in linear time. nil is a separator and
// cannot match a content hash or another nil.
func prefixMatches(values [][]byte) []int {
	z := make([]int, len(values))
	left, right := 0, 0
	for i := 1; i < len(values); i++ {
		if i < right {
			z[i] = min(right-i, z[i-left])
		}
		for i+z[i] < len(values) && values[z[i]] != nil && values[i+z[i]] != nil && bytes.Equal(values[z[i]], values[i+z[i]]) {
			z[i]++
		}
		if i+z[i] > right {
			left, right = i, i+z[i]
		}
	}
	return z
}
