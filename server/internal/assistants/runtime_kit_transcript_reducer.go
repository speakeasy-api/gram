package assistants

import (
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
)

// kitTranscriptReducer belongs to one ordered connection, not to the backend.
// Pinned Kit normally sends only chunks, with no final full agent_message.
// Reconstruct replacements from this connection's stream so journal replay
// starts from empty state instead of appending replayed chunks to stored text.
// The protocol must replay from start before consuming new work on a session.
type kitTranscriptReducer struct {
	messages      map[string][]json.RawMessage
	retainedBytes int
	lastMessage   map[string]string
}

func newKitTranscriptReducer() *kitTranscriptReducer {
	return &kitTranscriptReducer{messages: make(map[string][]json.RawMessage), lastMessage: make(map[string]string), retainedBytes: 0}
}

func (r *kitTranscriptReducer) Apply(n kitacp.Notification) (kitacp.Notification, error) {
	if n.Method != "session/update" {
		return n, nil
	}
	var envelope struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(n.Params, &envelope); err != nil {
		return n, fmt.Errorf("decode kit reducer envelope: %w", err)
	}
	var update kitTranscriptUpdate
	if err := json.Unmarshal(envelope.Update, &update); err != nil {
		return n, fmt.Errorf("decode kit reducer update: %w", err)
	}
	if update.SessionUpdate == "user_message" {
		delete(r.lastMessage, envelope.SessionID)
	}
	// Foreground terminal reasons belong only to the last assistant message
	// observed in this session's current turn. Idle without a reason is not success.
	if update.SessionUpdate == "state_update" && update.State == "idle" {
		messageID := r.lastMessage[envelope.SessionID]
		delete(r.lastMessage, envelope.SessionID)
		if update.StopReason != "" && update.MessageID == "" && messageID != "" {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(envelope.Update, &fields); err != nil {
				return n, fmt.Errorf("decode kit terminal update: %w", err)
			}
			fields["messageId"], _ = json.Marshal(messageID)
			envelope.Update, _ = json.Marshal(fields)
			n.Params, _ = json.Marshal(envelope)
		}
		return n, nil
	}
	if update.SessionUpdate != "agent_message" && update.SessionUpdate != "agent_message_chunk" {
		return n, nil
	}
	if update.MessageID == "" {
		return n, fmt.Errorf("kit assistant update requires stable message ID")
	}
	key := envelope.SessionID + "\x00" + update.MessageID
	if update.SessionUpdate == "agent_message" {
		if len(update.Content) != 0 {
			var blocks []json.RawMessage
			if err := json.Unmarshal(update.Content, &blocks); err != nil {
				return n, fmt.Errorf("decode kit replacement content: %w", err)
			}
			if err := r.replace(key, blocks); err != nil {
				return n, err
			}
		}
		if _, exists := r.messages[key]; exists {
			r.lastMessage[envelope.SessionID] = update.MessageID
		}
		return n, nil
	}
	blocks := append([]json.RawMessage(nil), r.messages[key]...)
	var chunk struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(update.Content, &chunk); err != nil {
		return n, fmt.Errorf("decode kit text delta: %w", err)
	}
	if chunk.Type == "" {
		return n, fmt.Errorf("kit content delta requires type")
	}
	merged := false
	if chunk.Type == "text" && len(blocks) > 0 {
		var previous struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(blocks[len(blocks)-1], &previous); err != nil {
			return n, fmt.Errorf("decode previous kit content: %w", err)
		}
		if previous.Type == "text" {
			previous.Text += chunk.Text
			encoded, err := json.Marshal(previous)
			if err != nil {
				return n, fmt.Errorf("encode merged kit text: %w", err)
			}
			blocks[len(blocks)-1] = encoded
			merged = true
		}
	}
	if !merged {
		blocks = append(blocks, append(json.RawMessage(nil), update.Content...))
	}
	if err := r.replace(key, blocks); err != nil {
		return n, err
	}
	r.lastMessage[envelope.SessionID] = update.MessageID
	content, err := json.Marshal(blocks)
	if err != nil {
		return n, fmt.Errorf("encode kit replacement content: %w", err)
	}
	// Keep optional extension fields on the event while replacing only its kind
	// and content. Unknown content blocks remain lossless canonical JSON text.
	var canonical map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Update, &canonical); err != nil {
		return n, fmt.Errorf("decode kit canonical update: %w", err)
	}
	canonical["sessionUpdate"] = json.RawMessage(`"agent_message"`)
	canonical["content"] = content
	envelope.Update, err = json.Marshal(canonical)
	if err != nil {
		return n, fmt.Errorf("encode kit canonical update: %w", err)
	}
	n.Params, err = json.Marshal(envelope)
	if err != nil {
		return n, fmt.Errorf("encode kit canonical notification: %w", err)
	}
	return n, nil
}

// Bound both payload and per-message overhead over the entire replay connection.
// Eviction is unsafe: later chunks must replace the complete prior message.
const kitTranscriptMaxMessages = 4096
const kitTranscriptMaxRetainedBytes = 64 << 20

func (r *kitTranscriptReducer) replace(key string, blocks []json.RawMessage) error {
	old, exists := r.messages[key]
	if !exists && len(r.messages) >= kitTranscriptMaxMessages {
		return fmt.Errorf("kit transcript message limit exceeded")
	}
	size := r.retainedBytes
	if !exists {
		size += len(key)
	}
	for _, block := range old {
		size -= len(block)
	}
	for _, block := range blocks {
		size += len(block)
	}
	if size > kitTranscriptMaxRetainedBytes {
		return fmt.Errorf("kit transcript retained content limit exceeded")
	}
	r.messages[key], r.retainedBytes = blocks, size
	return nil
}
