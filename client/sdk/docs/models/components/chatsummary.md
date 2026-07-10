# ChatSummary

Summary information for a chat session

## Example Usage

```typescript
import { ChatSummary } from "@gram/client/models/components/chatsummary.js";

let value: ChatSummary = {
  durationSeconds: 1264.89,
  endTimeUnixNano: "<value>",
  gramChatId: "<id>",
  logCount: 694611,
  messageCount: 634346,
  startTimeUnixNano: "<value>",
  status: "success",
  toolCallCount: 992597,
  totalInputTokens: 31568,
  totalOutputTokens: 867143,
  totalTokens: 581909,
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `durationSeconds`                                                            | *number*                                                                     | :heavy_check_mark:                                                           | Chat session duration in seconds                                             |
| `endTimeUnixNano`                                                            | *string*                                                                     | :heavy_check_mark:                                                           | Latest log timestamp in Unix nanoseconds (string for JS int64 precision)     |
| `gramChatId`                                                                 | *string*                                                                     | :heavy_check_mark:                                                           | Chat session ID                                                              |
| `logCount`                                                                   | *number*                                                                     | :heavy_check_mark:                                                           | Total number of logs in this chat session                                    |
| `messageCount`                                                               | *number*                                                                     | :heavy_check_mark:                                                           | Number of LLM completion messages in this chat session                       |
| `model`                                                                      | *string*                                                                     | :heavy_minus_sign:                                                           | LLM model used in this chat session                                          |
| `startTimeUnixNano`                                                          | *string*                                                                     | :heavy_check_mark:                                                           | Earliest log timestamp in Unix nanoseconds (string for JS int64 precision)   |
| `status`                                                                     | [components.ChatSummaryStatus](../../models/components/chatsummarystatus.md) | :heavy_check_mark:                                                           | Chat session status                                                          |
| `toolCallCount`                                                              | *number*                                                                     | :heavy_check_mark:                                                           | Number of tool calls in this chat session                                    |
| `totalInputTokens`                                                           | *number*                                                                     | :heavy_check_mark:                                                           | Total input tokens used                                                      |
| `totalOutputTokens`                                                          | *number*                                                                     | :heavy_check_mark:                                                           | Total output tokens used                                                     |
| `totalTokens`                                                                | *number*                                                                     | :heavy_check_mark:                                                           | Total tokens used (input + output)                                           |
| `userId`                                                                     | *string*                                                                     | :heavy_minus_sign:                                                           | User ID associated with this chat session                                    |