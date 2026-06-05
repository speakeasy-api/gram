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
