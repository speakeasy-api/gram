package risk_analysis

import (
	"slices"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func filterMessagesByMessageTypes(messages []repo.GetMessageContentBatchRow, messageTypes []string) []repo.GetMessageContentBatchRow {
	filtered := make([]repo.GetMessageContentBatchRow, 0, len(messages))
	for _, msg := range messages {
		messageType, ok := messageRowMessageType(msg)
		if !ok {
			continue
		}
		if len(messageTypes) > 0 && !slices.Contains(messageTypes, messageType) {
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}
func messageRowMessageType(msg repo.GetMessageContentBatchRow) (message.Type, bool) {
	switch msg.Role {
	case "user":
		return message.User, true
	case "tool":
		return message.ToolResponse, true
	case "assistant":
		if len(msg.ToolCalls) > 0 {
			return message.ToolRequest, true
		}
		return message.Assistant, true
	default:
		return "", false
	}
}
