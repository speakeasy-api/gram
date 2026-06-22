package assistants

import (
	"fmt"

	"github.com/google/uuid"
)

// sourceAdapter renders the per-source pieces of an assistant turn: the
// conversation-context header, the output-channel guidance, the decoded turn
// text, and the chat-id derivation. Each external source (Slack, Linear,
// GitHub, cron, wake) plus the dashboard implements one; the concrete adapters
// live in the source_adapter_<source>.go files alongside this shared core.
type sourceAdapter interface {
	ThreadContext(sourceRefJSON []byte) (string, error)
	OutputChannelGuidance() string
	DecodeTurn(event assistantThreadEventRecord) (string, error)
	// ChatID derives the conversation identity from a turn's correlation key.
	// External sources hash an opaque correlation key into a stable id; the
	// dashboard's correlation key already IS the chat id (server-minted on the
	// first turn and round-tripped by the client).
	ChatID(assistantID uuid.UUID, correlationID string) uuid.UUID
}

// deterministicChatIDAdapter is the default ChatID strategy embedded by every
// source whose correlation key is opaque (Slack, cron, wake). The dashboard
// source overrides ChatID because its correlation key already IS the chat id.
type deterministicChatIDAdapter struct{}

func (deterministicChatIDAdapter) ChatID(assistantID uuid.UUID, correlationID string) uuid.UUID {
	return deterministicChatID(assistantID, correlationID)
}

var sourceAdapters = map[string]sourceAdapter{
	sourceKindSlack:     slackAdapter{deterministicChatIDAdapter: deterministicChatIDAdapter{}},
	sourceKindLinear:    linearAdapter{deterministicChatIDAdapter: deterministicChatIDAdapter{}},
	sourceKindGithub:    githubAdapter{deterministicChatIDAdapter: deterministicChatIDAdapter{}},
	sourceKindCron:      cronAdapter{deterministicChatIDAdapter: deterministicChatIDAdapter{}},
	sourceKindWake:      wakeAdapter{deterministicChatIDAdapter: deterministicChatIDAdapter{}},
	sourceKindDashboard: dashboardAdapter{},
}

func getSourceAdapter(kind string) (sourceAdapter, error) {
	adapter, ok := sourceAdapters[kind]
	if !ok {
		return nil, fmt.Errorf("assistant source %q is not supported", kind)
	}
	return adapter, nil
}
