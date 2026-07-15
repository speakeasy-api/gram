# CodexHookPayload

Payload for Codex hook events

## Example Usage

```typescript
import { CodexHookPayload } from "@gram/client/models/components/codexhookpayload.js";

let value: CodexHookPayload = {
  hookEventName: "PostToolUse",
};
```

## Fields

| Field                  | Type                                                                                                 | Required           | Description                                                     |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------- |
| `additionalData`       | Record<string, _any_>                                                                                | :heavy_minus_sign: | Additional hook-specific data                                   |
| `cwd`                  | _string_                                                                                             | :heavy_minus_sign: | The working directory when the event fired                      |
| `hookEventName`        | [components.CodexHookPayloadHookEventName](../../models/components/codexhookpayloadhookeventname.md) | :heavy_check_mark: | The type of hook event                                          |
| `lastAssistantMessage` | _string_                                                                                             | :heavy_minus_sign: | The final assistant message text for the turn (Stop only)       |
| `model`                | _string_                                                                                             | :heavy_minus_sign: | The model identifier                                            |
| `permissionType`       | _string_                                                                                             | :heavy_minus_sign: | The type of permission being requested (PermissionRequest only) |
| `prompt`               | _string_                                                                                             | :heavy_minus_sign: | The user's prompt text (UserPromptSubmit only)                  |
| `sessionId`            | _string_                                                                                             | :heavy_minus_sign: | The Codex session ID                                            |
| `toolInput`            | _any_                                                                                                | :heavy_minus_sign: | The input to the tool (PreToolUse only)                         |
| `toolName`             | _string_                                                                                             | :heavy_minus_sign: | The name of the tool                                            |
| `toolOutput`           | _any_                                                                                                | :heavy_minus_sign: | The output from the tool (PostToolUse only)                     |
| `transcriptPath`       | _string_                                                                                             | :heavy_minus_sign: | Path to the conversation transcript file                        |
| `userEmail`            | _string_                                                                                             | :heavy_minus_sign: | Email of the authenticated Codex user, if available             |
