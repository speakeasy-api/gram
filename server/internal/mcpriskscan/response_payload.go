package mcpriskscan

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// ParseToolResultPayload extracts textual and structured tool result content.
func ParseToolResultPayload(data []byte) (Payload, error) {
	var result struct {
		Content           []json.RawMessage `json:"content"`
		StructuredContent json.RawMessage   `json:"structuredContent"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Payload{}, fmt.Errorf("decode MCP tool result: %w", err)
	}
	return ToolResultPayload(result.Content, result.StructuredContent)
}

// ToolResultPayload extracts textual content blocks and structured content.
func ToolResultPayload(contentBlocks []json.RawMessage, structuredContent json.RawMessage) (Payload, error) {
	parts := make([][]byte, 0, len(contentBlocks)+1)
	for _, raw := range contentBlocks {
		if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var content struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Resource *struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
				Blob string `json:"blob"`
			} `json:"resource"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			return Payload{}, fmt.Errorf("decode MCP tool result content: %w", err)
		}
		switch content.Type {
		case "text":
			parts = append(parts, []byte(content.Text))
		case "resource":
			if content.Resource != nil && content.Resource.Blob == "" {
				parts = append(parts, []byte(content.Resource.URI), []byte(content.Resource.Text))
			}
		}
	}
	if structured := bytes.TrimSpace(structuredContent); len(structured) > 0 && !bytes.Equal(structured, []byte("null")) {
		parts = append(parts, structured)
	}
	return responsePayload(parts), nil
}

// ParseResourceResultPayload extracts text resources and skips binary blobs.
func ParseResourceResultPayload(data []byte) (Payload, error) {
	var result struct {
		Contents []struct {
			Text string `json:"text"`
			Blob string `json:"blob"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Payload{}, fmt.Errorf("decode MCP resource result: %w", err)
	}

	parts := make([][]byte, 0, len(result.Contents))
	for _, content := range result.Contents {
		if content.Blob == "" {
			parts = append(parts, []byte(content.Text))
		}
	}
	return responsePayload(parts), nil
}

// TextResponsePayload creates a response payload from one textual result.
func TextResponsePayload(data []byte) Payload {
	return responsePayload([][]byte{data})
}

// JSONRPCErrorPayload extracts error message and structured data without the envelope.
func JSONRPCErrorPayload(rpcErr *jsonrpc.Error) Payload {
	if rpcErr == nil {
		return BorrowPayload(nil)
	}
	parts := [][]byte{[]byte(rpcErr.Message)}
	if data := bytes.TrimSpace(rpcErr.Data); len(data) > 0 && !bytes.Equal(data, []byte("null")) {
		parts = append(parts, data)
	}
	return responsePayload(parts)
}

func responsePayload(parts [][]byte) Payload {
	totalBytes := 0
	partCount := 0
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if partCount > 0 {
			totalBytes++
		}
		partCount++
		totalBytes += len(part)
	}
	if totalBytes == 0 {
		return BorrowPayload([]byte{})
	}
	if totalBytes > MaxPayloadBytes {
		return Payload{data: nil, availability: PayloadOversized}
	}
	data := make([]byte, 0, totalBytes)
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if len(data) > 0 {
			data = append(data, '\n')
		}
		data = append(data, part...)
	}
	return BorrowPayload(data)
}
