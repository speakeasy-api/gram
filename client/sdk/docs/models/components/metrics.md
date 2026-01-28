# Metrics

Aggregated metrics

## Example Usage

```typescript
import { Metrics } from "@gram/client/models/components";

let value: Metrics = {
  avgChatDurationMs: 5975.43,
  avgTokensPerRequest: 7403.64,
  avgToolDurationMs: 2776.12,
  distinctModels: 728402,
  distinctProviders: 292527,
  finishReasonStop: 369765,
  finishReasonToolCalls: 705473,
  models: [
    {
      count: 645529,
      name: "<value>",
    },
  ],
  toolCallFailure: 830489,
  toolCallSuccess: 950269,
  tools: [],
  totalChatRequests: 127608,
  totalChats: 874173,
  totalInputTokens: 83595,
  totalOutputTokens: 489312,
  totalTokens: 388106,
  totalToolCalls: 100457,
};
```

## Fields

| Field                                                            | Type                                                             | Required                                                         | Description                                                      |
| ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- |
| `avgChatDurationMs`                                              | *number*                                                         | :heavy_check_mark:                                               | Average chat request duration in milliseconds                    |
| `avgTokensPerRequest`                                            | *number*                                                         | :heavy_check_mark:                                               | Average tokens per chat request                                  |
| `avgToolDurationMs`                                              | *number*                                                         | :heavy_check_mark:                                               | Average tool call duration in milliseconds                       |
| `distinctModels`                                                 | *number*                                                         | :heavy_check_mark:                                               | Number of distinct models used (project scope only)              |
| `distinctProviders`                                              | *number*                                                         | :heavy_check_mark:                                               | Number of distinct providers used (project scope only)           |
| `finishReasonStop`                                               | *number*                                                         | :heavy_check_mark:                                               | Requests that completed naturally                                |
| `finishReasonToolCalls`                                          | *number*                                                         | :heavy_check_mark:                                               | Requests that resulted in tool calls                             |
| `models`                                                         | [components.ModelUsage](../../models/components/modelusage.md)[] | :heavy_check_mark:                                               | List of models used with call counts                             |
| `toolCallFailure`                                                | *number*                                                         | :heavy_check_mark:                                               | Failed tool calls (4xx/5xx status)                               |
| `toolCallSuccess`                                                | *number*                                                         | :heavy_check_mark:                                               | Successful tool calls (2xx status)                               |
| `tools`                                                          | [components.ToolUsage](../../models/components/toolusage.md)[]   | :heavy_check_mark:                                               | List of tools used with success/failure counts                   |
| `totalChatRequests`                                              | *number*                                                         | :heavy_check_mark:                                               | Total number of chat requests                                    |
| `totalChats`                                                     | *number*                                                         | :heavy_check_mark:                                               | Number of unique chat sessions (project scope only)              |
| `totalInputTokens`                                               | *number*                                                         | :heavy_check_mark:                                               | Sum of input tokens used                                         |
| `totalOutputTokens`                                              | *number*                                                         | :heavy_check_mark:                                               | Sum of output tokens used                                        |
| `totalTokens`                                                    | *number*                                                         | :heavy_check_mark:                                               | Sum of all tokens used                                           |
| `totalToolCalls`                                                 | *number*                                                         | :heavy_check_mark:                                               | Total number of tool calls                                       |