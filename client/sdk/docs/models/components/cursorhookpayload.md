# CursorHookPayload

Payload for Cursor hook events

## Example Usage

```typescript
import { CursorHookPayload } from "@gram/client/models/components/cursorhookpayload.js";

let value: CursorHookPayload = {
  hookEventName: "<value>",
};
```

## Fields

| Field              | Type                  | Required           | Description                                                                                                                                                                       |
| ------------------ | --------------------- | ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `additionalData`   | Record<string, _any_> | :heavy_minus_sign: | Additional hook-specific data                                                                                                                                                     |
| `cacheReadTokens`  | _number_              | :heavy_minus_sign: | Tokens read from cache (stop, afterAgentResponse)                                                                                                                                 |
| `cacheWriteTokens` | _number_              | :heavy_minus_sign: | Tokens written to cache (stop, afterAgentResponse)                                                                                                                                |
| `command`          | _string_              | :heavy_minus_sign: | Command string for command-based MCP servers (beforeMCPExecution / afterMCPExecution only)                                                                                        |
| `composerMode`     | _string_              | :heavy_minus_sign: | The composer mode, e.g. agent (beforeSubmitPrompt only)                                                                                                                           |
| `conversationId`   | _string_              | :heavy_minus_sign: | The Cursor conversation ID                                                                                                                                                        |
| `cursorVersion`    | _string_              | :heavy_minus_sign: | The Cursor IDE version                                                                                                                                                            |
| `duration`         | _number_              | :heavy_minus_sign: | Execution duration in milliseconds, excluding approval wait time (afterMCPExecution only)                                                                                         |
| `durationMs`       | _number_              | :heavy_minus_sign: | Duration in milliseconds for the thinking block (afterAgentThought only)                                                                                                          |
| `error`            | _any_                 | :heavy_minus_sign: | The error from the tool (postToolUseFailure only)                                                                                                                                 |
| `generationId`     | _string_              | :heavy_minus_sign: | The Cursor generation ID                                                                                                                                                          |
| `hookEventName`    | _string_              | :heavy_check_mark: | The type of hook event (e.g. beforeSubmitPrompt, stop, afterAgentResponse, afterAgentThought, preToolUse, postToolUse, postToolUseFailure, beforeMCPExecution, afterMCPExecution) |
| `inputTokens`      | _number_              | :heavy_minus_sign: | Total input tokens used (stop, afterAgentResponse)                                                                                                                                |
| `isInterrupt`      | _boolean_             | :heavy_minus_sign: | Whether the failure was caused by user interruption                                                                                                                               |
| `loopCount`        | _number_              | :heavy_minus_sign: | Number of agentic loops executed (stop only)                                                                                                                                      |
| `model`            | _string_              | :heavy_minus_sign: | The model being used                                                                                                                                                              |
| `outputTokens`     | _number_              | :heavy_minus_sign: | Total output tokens used (stop, afterAgentResponse)                                                                                                                               |
| `prompt`           | _string_              | :heavy_minus_sign: | The user's prompt text (beforeSubmitPrompt only)                                                                                                                                  |
| `resultJson`       | _string_              | :heavy_minus_sign: | JSON-encoded string of the MCP tool response (afterMCPExecution only)                                                                                                             |
| `sessionId`        | _string_              | :heavy_minus_sign: | The session ID from Cursor                                                                                                                                                        |
| `status`           | _string_              | :heavy_minus_sign: | Completion status, e.g. completed (stop only)                                                                                                                                     |
| `text`             | _string_              | :heavy_minus_sign: | The assistant's response text (afterAgentResponse) or thinking text (afterAgentThought)                                                                                           |
| `toolInput`        | _any_                 | :heavy_minus_sign: | The input to the tool                                                                                                                                                             |
| `toolName`         | _string_              | :heavy_minus_sign: | The name of the tool                                                                                                                                                              |
| `toolResponse`     | _any_                 | :heavy_minus_sign: | The response from the tool (postToolUse only)                                                                                                                                     |
| `toolUseId`        | _string_              | :heavy_minus_sign: | The unique ID for this tool use                                                                                                                                                   |
| `transcriptPath`   | _string_              | :heavy_minus_sign: | Path to the conversation transcript JSONL file                                                                                                                                    |
| `url`              | _string_              | :heavy_minus_sign: | URL of the MCP server (beforeMCPExecution / afterMCPExecution, URL-based servers only)                                                                                            |
| `userEmail`        | _string_              | :heavy_minus_sign: | Email of the authenticated Cursor user, if available                                                                                                                              |
