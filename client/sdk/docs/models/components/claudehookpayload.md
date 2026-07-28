# ClaudeHookPayload

Unified payload for all Claude Code hook events

## Example Usage

```typescript
import { ClaudeHookPayload } from "@gram/client/models/components/claudehookpayload.js";

let value: ClaudeHookPayload = {
  hookEventName: "SessionStart",
};
```

## Fields

| Field                  | Type                                                                 | Required           | Description                                                                                                |
| ---------------------- | -------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------- |
| `additionalData`       | Record<string, _any_>                                                | :heavy_minus_sign: | Additional hook-specific data                                                                              |
| `cwd`                  | _string_                                                             | :heavy_minus_sign: | The working directory when the event fired                                                                 |
| `error`                | _any_                                                                | :heavy_minus_sign: | The error from the tool (PostToolUseFailure only)                                                          |
| `hookEventName`        | [components.HookEventName](../../models/components/hookeventname.md) | :heavy_check_mark: | The type of hook event                                                                                     |
| `isInterrupt`          | _boolean_                                                            | :heavy_minus_sign: | Whether the failure was caused by user interruption (PostToolUseFailure only)                              |
| `lastAssistantMessage` | _string_                                                             | :heavy_minus_sign: | Claude's final response text (Stop only)                                                                   |
| `message`              | _string_                                                             | :heavy_minus_sign: | Notification message text (Notification only)                                                              |
| `model`                | _string_                                                             | :heavy_minus_sign: | The model identifier (SessionStart, Stop)                                                                  |
| `notificationType`     | _string_                                                             | :heavy_minus_sign: | Type of notification: permission_prompt, idle_prompt, auth_success, elicitation_dialog (Notification only) |
| `prompt`               | _string_                                                             | :heavy_minus_sign: | The user's prompt text (UserPromptSubmit only)                                                             |
| `reason`               | _string_                                                             | :heavy_minus_sign: | Why the session ended (SessionEnd only)                                                                    |
| `sessionId`            | _string_                                                             | :heavy_minus_sign: | The Claude Code session ID                                                                                 |
| `source`               | _string_                                                             | :heavy_minus_sign: | How the session started: startup, resume, clear, compact (SessionStart only)                               |
| `stopHookActive`       | _boolean_                                                            | :heavy_minus_sign: | Whether a stop hook continuation is active (Stop only)                                                     |
| `title`                | _string_                                                             | :heavy_minus_sign: | Notification title (Notification only)                                                                     |
| `toolInput`            | _any_                                                                | :heavy_minus_sign: | The input to the tool                                                                                      |
| `toolName`             | _string_                                                             | :heavy_minus_sign: | The name of the tool (for tool-related events)                                                             |
| `toolResponse`         | _any_                                                                | :heavy_minus_sign: | The response from the tool (PostToolUse only)                                                              |
| `toolUseId`            | _string_                                                             | :heavy_minus_sign: | The unique ID for this tool use                                                                            |
| `transcriptPath`       | _string_                                                             | :heavy_minus_sign: | Path to the conversation transcript file                                                                   |
| `userEmail`            | _string_                                                             | :heavy_minus_sign: | Email of the authenticated user from the Speakeasy device agent, if available                              |
