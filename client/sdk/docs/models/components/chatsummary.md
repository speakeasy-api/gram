# ChatSummary

Summary information for a chat session

## Example Usage

```typescript
import { ChatSummary } from "@gram/client/models/components";

let value: ChatSummary = {
  durationSeconds: 1264.89,
  endTimeUnixNano: 694611,
  gramChatId: "<id>",
  logCount: 634346,
  messageCount: 97514,
  startTimeUnixNano: 992597,
  status: "success",
  toolCallCount: 867143,
  totalInputTokens: 581909,
  totalOutputTokens: 558897,
  totalTokens: 62335,
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `durationSeconds`                                                            | *number*                                                                     | :heavy_check_mark:                                                           | Chat session duration in seconds                                             |
| `endTimeUnixNano`                                                            | *number*                                                                     | :heavy_check_mark:                                                           | Latest log timestamp in Unix nanoseconds                                     |
| `gramChatId`                                                                 | *string*                                                                     | :heavy_check_mark:                                                           | Chat session ID                                                              |
| `logCount`                                                                   | *number*                                                                     | :heavy_check_mark:                                                           | Total number of logs in this chat session                                    |
| `messageCount`                                                               | *number*                                                                     | :heavy_check_mark:                                                           | Number of LLM completion messages in this chat session                       |
| `model`                                                                      | *string*                                                                     | :heavy_minus_sign:                                                           | LLM model used in this chat session                                          |
| `startTimeUnixNano`                                                          | *number*                                                                     | :heavy_check_mark:                                                           | Earliest log timestamp in Unix nanoseconds                                   |
| `status`                                                                     | [components.ChatSummaryStatus](../../models/components/chatsummarystatus.md) | :heavy_check_mark:                                                           | Chat session status                                                          |
| `toolCallCount`                                                              | *number*                                                                     | :heavy_check_mark:                                                           | Number of tool calls in this chat session                                    |
| `totalInputTokens`                                                           | *number*                                                                     | :heavy_check_mark:                                                           | Total input tokens used                                                      |
| `totalOutputTokens`                                                          | *number*                                                                     | :heavy_check_mark:                                                           | Total output tokens used                                                     |
| `totalTokens`                                                                | *number*                                                                     | :heavy_check_mark:                                                           | Total tokens used (input + output)                                           |
| `userId`                                                                     | *string*                                                                     | :heavy_minus_sign:                                                           | User ID associated with this chat session                                    |