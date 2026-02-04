package resolution_activities

import (
	"fmt"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// formatChatMessages formats a slice of chat messages for LLM analysis.
// It includes time gaps between messages and redacts system prompts.
func formatChatMessages(messages []repo.ChatMessage) string {
	var sb strings.Builder

	for i, msg := range messages {
		// Calculate time gap from previous message
		var timeGap string
		if i > 0 && msg.CreatedAt.Valid && messages[i-1].CreatedAt.Valid {
			gap := msg.CreatedAt.Time.Sub(messages[i-1].CreatedAt.Time)
			if gap > time.Hour {
				timeGap = fmt.Sprintf(" [+%.1f hours since previous]", gap.Hours())
			} else if gap > time.Minute {
				timeGap = fmt.Sprintf(" [+%.1f minutes since previous]", gap.Minutes())
			} else if gap > time.Second {
				timeGap = fmt.Sprintf(" [+%.0f seconds since previous]", gap.Seconds())
			}
		}

		content := msg.Content
		if msg.Role == "system" {
			content = "[original system prompt redacted]"
		}

		sb.WriteString(fmt.Sprintf("[%d] %s%s: %s\n\n", i, msg.Role, timeGap, content))
	}

	return sb.String()
}
