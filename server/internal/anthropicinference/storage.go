package anthropicinference

import (
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

type storedBlock struct {
	role       string
	content    string
	toolCalls  []byte
	toolCallID string
}

type storedToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type storedToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function storedToolFunction `json:"function"`
}

// Split mixed messages so each stored row has the role and tool representation
// understood by conversation readers, metering, and asynchronous policy scans.
func storageBlocks(msg Message, identity string) ([]storedBlock, []contentBlock, error) {
	blocks, err := knownBlocks(msg.Content)
	if err != nil {
		return nil, nil, err
	}
	rows := make([]storedBlock, 0, len(blocks))
	var attachments []contentBlock
	for index, block := range blocks {
		row := storedBlock{role: msg.Role, content: "", toolCalls: nil, toolCallID: ""}
		switch block.Type {
		case "text":
			row.content = block.Text
		case "attachment":
			attachments = append(attachments, block)
			continue
		case "tool_use":
			id := conv.Default(block.ID, fmt.Sprintf("%s/tool:%d", identity, index))
			calls, err := json.Marshal([]storedToolCall{{ID: id, Type: "function", Function: storedToolFunction{Name: conv.Default(block.ToolName, block.Name), Arguments: string(block.Input)}}})
			if err != nil {
				return nil, nil, fmt.Errorf("encode inference tool call: %w", err)
			}
			row.role, row.toolCalls = "assistant", calls
		case "tool_result":
			row.role, row.content, row.toolCallID = "tool", block.Content, block.ToolUseID
		}
		rows = append(rows, row)
	}
	// Keep one parent/ordinal for attachment-only and forward-compatible messages.
	if len(rows) == 0 {
		rows = append(rows, storedBlock{role: msg.Role, content: "", toolCalls: nil, toolCallID: ""})
	}
	return rows, attachments, nil
}
