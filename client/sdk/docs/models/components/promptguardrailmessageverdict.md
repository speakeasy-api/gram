# PromptGuardrailMessageVerdict

The LLM judge's verdict for one in-scope message in the replayed session.

## Example Usage

```typescript
import { PromptGuardrailMessageVerdict } from "@gram/client/models/components/promptguardrailmessageverdict.js";

let value: PromptGuardrailMessageVerdict = {
  completionTokens: 758937,
  confidence: 6867.83,
  costUsd: 1480.56,
  latencyMs: 257342,
  matched: true,
  messageId: "74aae6ff-d4a7-4f92-b6e1-62e529a09b31",
  messageType: "<value>",
  promptTokens: 297244,
  rationale: "<value>",
  seq: 667274,
  totalTokens: 526882,
};
```

## Fields

| Field              | Type      | Required           | Description                                                                             |
| ------------------ | --------- | ------------------ | --------------------------------------------------------------------------------------- |
| `completionTokens` | _number_  | :heavy_check_mark: | Completion tokens billed for this judge call.                                           |
| `confidence`       | _number_  | :heavy_check_mark: | Judge confidence in [0,1]; 0 when not matched.                                          |
| `costUsd`          | _number_  | :heavy_check_mark: | OpenRouter cost for judging this message, in USD. Zero when cost was not returned.      |
| `latencyMs`        | _number_  | :heavy_check_mark: | Wall-clock latency for judging this message, in milliseconds.                           |
| `matched`          | _boolean_ | :heavy_check_mark: | True when the guardrail flagged this message.                                           |
| `messageId`        | _string_  | :heavy_check_mark: | The chat message ID.                                                                    |
| `messageType`      | _string_  | :heavy_check_mark: | The judged message type (user_message, assistant_message, tool_request, tool_response). |
| `promptTokens`     | _number_  | :heavy_check_mark: | Prompt tokens billed for this judge call.                                               |
| `rationale`        | _string_  | :heavy_check_mark: | One-sentence judge rationale; empty when not matched.                                   |
| `seq`              | _number_  | :heavy_check_mark: | Message sequence within the chat generation, ascending.                                 |
| `toolName`         | _string_  | :heavy_minus_sign: | Tool name for a single-call tool_request message; empty otherwise.                      |
| `totalTokens`      | _number_  | :heavy_check_mark: | Total tokens billed for this judge call.                                                |
