// Package anthropicinference receives Anthropic Enterprise inference hooks.
package anthropicinference

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

const maxRequestBytes = 10 * 1024 * 1024

// Frame is the transcript Anthropic sends before inference.
type Frame struct {
	// Type identifies the hook event; unknown events are forward compatible.
	Type string `json:"type"`

	// RequestID must equal the signed webhook-id header.
	RequestID string `json:"request_id"`

	// TenantID is the Anthropic organization asserted by the signature.
	TenantID string `json:"tenant_id"`

	// Actor is the principal associated with this inference.
	Actor Actor `json:"actor"`

	// Source is advisory application metadata.
	Source Source `json:"source"`

	// Messages is the complete user-visible transcript before inference.
	Messages []Message `json:"messages"`

	// SessionID is opaque and may be absent.
	SessionID string `json:"session_id"`

	// Model is the optional public model identifier.
	Model string `json:"model"`
}

// Actor describes the principal asserted by the signed request.
type Actor struct {
	// Type discriminates the actor union.
	Type string `json:"type"`

	// ID is the stable provider account identifier when available.
	ID string `json:"id"`

	// EmailAddress may resolve to a connected user in the bound organization.
	EmailAddress string `json:"email_address"`
}

// Source identifies the originating application; it is advisory metadata.
type Source struct {
	// Application names the originating product and is not an authorization signal.
	Application string `json:"application"`
}

// Message preserves the original content blocks, including future block types.
type Message struct {
	// Role identifies a user or assistant turn.
	Role string `json:"role"`

	// Content retains the original content blocks, including unknown types.
	Content json.RawMessage `json:"content"`
}

// Verdict is returned with HTTP 200 for both allowed and denied requests.
type Verdict struct {
	// Action is allow or deny.
	Action string `json:"action"`

	// DenyReason is safe end-user copy, bounded to 500 characters.
	DenyReason string `json:"deny_reason,omitempty"`

	// ReferenceID optionally correlates the verdict to an evaluation record.
	ReferenceID string `json:"reference_id,omitempty"`
}

// verifySignature authenticates the original body with any configured key,
// allowing an overlap during signing-secret rotation.
func verifySignature(headers http.Header, body []byte, keys [][]byte, now time.Time) error {
	id := headers.Get("webhook-id")
	timestamp := headers.Get("webhook-timestamp")
	signedAt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || id == "" {
		return errors.New("missing or invalid webhook identity")
	}
	if signedAt < now.Unix()-300 || signedAt > now.Unix()+300 {
		return errors.New("webhook timestamp outside tolerance")
	}
	for _, key := range keys {
		if len(key) == 0 {
			continue
		}
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(id + "." + timestamp + "."))
		_, _ = mac.Write(body)
		expected := mac.Sum(nil)
		for candidate := range strings.FieldsSeq(headers.Get("webhook-signature")) {
			version, signature, ok := strings.Cut(candidate, ",")
			if !ok || version != "v1" {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(signature)
			if err == nil && hmac.Equal(expected, decoded) {
				return nil
			}
		}
	}
	return errors.New("invalid webhook signature")
}

// contentBlock decodes known fields only after identifying a supported block.
type contentBlock struct {
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	FileName  string          `json:"file_name"`
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Content   string          `json:"content"`
	Name      string          `json:"name"`      // tool_use: standard Anthropic Messages API field
	ToolName  string          `json:"tool_name"` // tool_result: advisory correlation field
	Input     json.RawMessage `json:"input"`
}

func knownBlocks(content json.RawMessage) ([]contentBlock, error) {
	var rawBlocks []json.RawMessage
	if err := json.Unmarshal(content, &rawBlocks); err != nil {
		return nil, fmt.Errorf("decode inference content block: %w", err)
	}
	blocks := make([]contentBlock, 0, len(rawBlocks))
	for _, raw := range rawBlocks {
		var tag struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &tag); err != nil {
			return nil, fmt.Errorf("decode inference content block: %w", err)
		}
		switch tag.Type {
		case "text", "attachment", "tool_use", "tool_result":
			var block contentBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				return nil, fmt.Errorf("decode inference content block: %w", err)
			}
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

// messageText renders documented content while preserving unknown blocks in storage.
func messageText(message Message) (string, error) {
	blocks, err := knownBlocks(message.Content)
	if err != nil {
		return "", err
	}
	var texts []string
	for _, block := range blocks {
		switch block.Type {
		case "text", "attachment":
			texts = append(texts, block.Text)
		case "tool_result":
			texts = append(texts, block.ToolName+": "+block.Content)
		case "tool_use":
			texts = append(texts, conv.Default(block.ToolName, block.Name)+": "+string(block.Input))
		}
	}
	return strings.Join(texts, "\n"), nil
}

// DecodeSigningSecret validates Anthropic's Standard Webhooks signing secret.
func DecodeSigningSecret(secret string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil || len(key) < 16 {
		return nil, errors.New("invalid Anthropic signing secret")
	}
	return key, nil
}
