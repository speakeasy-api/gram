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

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `durationSeconds`                                                                  | *number*                                                                           | :heavy_check_mark:                                                                 | Chat session duration in seconds                                                   |
| `endTimeUnixNano`                                                                  | *string*                                                                           | :heavy_check_mark:                                                                 | Latest log timestamp in Unix nanoseconds (string for JS int64 precision)           |
| `gramChatId`                                                                       | *string*                                                                           | :heavy_check_mark:                                                                 | Chat session ID                                                                    |
| `hookSource`                                                                       | *string*                                                                           | :heavy_minus_sign:                                                                 | Client or agent surface associated with this chat session                          |
| `messageCount`                                                                     | *number*                                                                           | :heavy_check_mark:                                                                 | Number of LLM completion messages in this chat session                             |
| `model`                                                                            | *string*                                                                           | :heavy_minus_sign:                                                                 | LLM model used in this chat session                                                |
| `projectId`                                                                        | *string*                                                                           | :heavy_check_mark:                                                                 | Project ID that emitted this chat session                                          |
| `startTimeUnixNano`                                                                | *string*                                                                           | :heavy_check_mark:                                                                 | Earliest log timestamp in Unix nanoseconds (string for JS int64 precision)         |
| `status`                                                                           | [components.SessionSummaryStatus](../../models/components/sessionsummarystatus.md) | :heavy_check_mark:                                                                 | Chat session status                                                                |
| `title`                                                                            | *string*                                                                           | :heavy_minus_sign:                                                                 | Chat title, when the session resolves to a named chat                              |
| `toolCallCount`                                                                    | *number*                                                                           | :heavy_check_mark:                                                                 | Number of tool calls in this chat session                                          |
| `totalCost`                                                                        | *number*                                                                           | :heavy_check_mark:                                                                 | Total cost in USD                                                                  |
| `totalInputTokens`                                                                 | *number*                                                                           | :heavy_check_mark:                                                                 | Total input tokens used                                                            |
| `totalOutputTokens`                                                                | *number*                                                                           | :heavy_check_mark:                                                                 | Total output tokens used                                                           |
| `totalTokens`                                                                      | *number*                                                                           | :heavy_check_mark:                                                                 | Total tokens used                                                                  |
| `userEmail`                                                                        | *string*                                                                           | :heavy_minus_sign:                                                                 | User email associated with this chat session                                       |