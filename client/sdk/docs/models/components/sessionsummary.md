# SessionSummary

Org-scoped summary information for a chat session

## Example Usage

```typescript
import { SessionSummary } from "@gram/client/models/components/sessionsummary.js";

let value: SessionSummary = {
  durationSeconds: 4571.74,
  endTimeUnixNano: "<value>",
  gramChatId: "<id>",
  messageCount: 526072,
  projectId: "<id>",
  startTimeUnixNano: "<value>",
  status: "success",
  toolCallCount: 101482,
  totalCost: 2219.82,
  totalInputTokens: 112581,
  totalOutputTokens: 605493,
  totalTokens: 544314,
};
```

## Fields

| Field               | Type                                                                               | Required           | Description                                                                |
| ------------------- | ---------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------- |
| `durationSeconds`   | _number_                                                                           | :heavy_check_mark: | Chat session duration in seconds                                           |
| `endTimeUnixNano`   | _string_                                                                           | :heavy_check_mark: | Latest log timestamp in Unix nanoseconds (string for JS int64 precision)   |
| `gramChatId`        | _string_                                                                           | :heavy_check_mark: | Chat session ID                                                            |
| `hookSource`        | _string_                                                                           | :heavy_minus_sign: | Client or agent surface associated with this chat session                  |
| `messageCount`      | _number_                                                                           | :heavy_check_mark: | Number of LLM completion messages in this chat session                     |
| `model`             | _string_                                                                           | :heavy_minus_sign: | LLM model used in this chat session                                        |
| `projectId`         | _string_                                                                           | :heavy_check_mark: | Project ID that emitted this chat session                                  |
| `startTimeUnixNano` | _string_                                                                           | :heavy_check_mark: | Earliest log timestamp in Unix nanoseconds (string for JS int64 precision) |
| `status`            | [components.SessionSummaryStatus](../../models/components/sessionsummarystatus.md) | :heavy_check_mark: | Chat session status                                                        |
| `title`             | _string_                                                                           | :heavy_minus_sign: | Chat title, when the session resolves to a named chat                      |
| `toolCallCount`     | _number_                                                                           | :heavy_check_mark: | Number of tool calls in this chat session                                  |
| `totalCost`         | _number_                                                                           | :heavy_check_mark: | Total cost in USD                                                          |
| `totalInputTokens`  | _number_                                                                           | :heavy_check_mark: | Total input tokens used                                                    |
| `totalOutputTokens` | _number_                                                                           | :heavy_check_mark: | Total output tokens used                                                   |
| `totalTokens`       | _number_                                                                           | :heavy_check_mark: | Total tokens used                                                          |
| `userEmail`         | _string_                                                                           | :heavy_minus_sign: | User email associated with this chat session                               |
