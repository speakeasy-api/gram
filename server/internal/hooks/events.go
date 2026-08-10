package hooks

type HookEvent string

const (
	HookEventUnknown            HookEvent = ""
	HookEventSessionStart       HookEvent = "SessionStart"
	HookEventConfigChange       HookEvent = "ConfigChange"
	HookEventPreToolUse         HookEvent = "PreToolUse"
	HookEventPostToolUse        HookEvent = "PostToolUse"
	HookEventPostToolUseFailure HookEvent = "PostToolUseFailure"
	HookEventUserPromptSubmit   HookEvent = "UserPromptSubmit"
	HookEventStop               HookEvent = "Stop"
	HookEventSubagentStop       HookEvent = "SubagentStop"
	HookEventSessionEnd         HookEvent = "SessionEnd"
	HookEventNotification       HookEvent = "Notification"
	HookEventPermissionRequest  HookEvent = "PermissionRequest"
	HookEventBeforeSubmitPrompt HookEvent = "BeforeSubmitPrompt"
	HookEventAfterAgentResponse HookEvent = "AfterAgentResponse"
	HookEventAfterAgentThought  HookEvent = "AfterAgentThought"
	HookEventBeforeMCPExecution HookEvent = "BeforeMCPExecution"
	HookEventAfterMCPExecution  HookEvent = "AfterMCPExecution"
)

func parseClaudeHookEvent(raw string) (HookEvent, bool) {
	switch raw {
	case string(HookEventSessionStart):
		return HookEventSessionStart, true
	case string(HookEventConfigChange):
		return HookEventConfigChange, true
	case string(HookEventPreToolUse):
		return HookEventPreToolUse, true
	case string(HookEventPostToolUse):
		return HookEventPostToolUse, true
	case string(HookEventPostToolUseFailure):
		return HookEventPostToolUseFailure, true
	case string(HookEventUserPromptSubmit):
		return HookEventUserPromptSubmit, true
	case string(HookEventStop):
		return HookEventStop, true
	case string(HookEventSubagentStop):
		return HookEventSubagentStop, true
	case string(HookEventSessionEnd):
		return HookEventSessionEnd, true
	case string(HookEventNotification):
		return HookEventNotification, true
	default:
		return HookEventUnknown, false
	}
}

func parseCodexHookEvent(raw string) (HookEvent, bool) {
	switch raw {
	case string(HookEventSessionStart):
		return HookEventSessionStart, true
	case string(HookEventPreToolUse):
		return HookEventPreToolUse, true
	case string(HookEventPermissionRequest):
		return HookEventPermissionRequest, true
	case string(HookEventPostToolUse):
		return HookEventPostToolUse, true
	case string(HookEventUserPromptSubmit):
		return HookEventUserPromptSubmit, true
	case string(HookEventStop):
		return HookEventStop, true
	default:
		return HookEventUnknown, false
	}
}

func parseCursorHookEvent(raw string) (HookEvent, bool) {
	switch raw {
	case "beforeSubmitPrompt":
		return HookEventBeforeSubmitPrompt, true
	case "stop":
		return HookEventStop, true
	case "afterAgentResponse":
		return HookEventAfterAgentResponse, true
	case "afterAgentThought":
		return HookEventAfterAgentThought, true
	case "preToolUse":
		return HookEventPreToolUse, true
	case "postToolUse":
		return HookEventPostToolUse, true
	case "postToolUseFailure":
		return HookEventPostToolUseFailure, true
	case "beforeMCPExecution":
		return HookEventBeforeMCPExecution, true
	case "afterMCPExecution":
		return HookEventAfterMCPExecution, true
	default:
		return HookEventUnknown, false
	}
}

// parseOpencodeHookEvent maps source.raw_event_name values to canonical
// HookEvent names. These are agenthooks' native
// NativeNames (see codec_opencode.go's opencodeKind) — the OpenCode SDK's own
// hook/event type strings, not synthesized names.
func parseOpencodeHookEvent(raw string) (HookEvent, bool) {
	switch raw {
	case "session.created":
		return HookEventSessionStart, true
	// session.idle and message.part.updated are intentionally not mapped here.
	// agenthooks already classifies them: a finished turn is decoded as KindStop
	// (canonical assistant.responded/usage.reported), and a failed tool call is
	// lifted from message.part.updated only when the part is a tool in error
	// state (decodeOpenCodeToolError -> KindToolError -> canonical tool.failed).
	// Every other message.part.updated is streaming noise (KindOther ->
	// session.updated). Re-deriving from the raw name would mark all of them as
	// failures, so these fall through to the canonical Event.Type in
	// telemetryHookEventName instead.
	case "server.instance.disposed":
		return HookEventSessionEnd, true
	case "tool.execute.before":
		return HookEventPreToolUse, true
	case "tool.execute.after":
		return HookEventPostToolUse, true
	case "chat.message":
		return HookEventUserPromptSubmit, true
	case "permission.asked":
		return HookEventPermissionRequest, true
	default:
		return HookEventUnknown, false
	}
}
