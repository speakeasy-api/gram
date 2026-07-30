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
  firstSeenUnixNano: "<value>",
  lastSeenUnixNano: "<value>",
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

| Field                   | Type                                                             | Required           | Description                                            |
| ----------------------- | ---------------------------------------------------------------- | ------------------ | ------------------------------------------------------ |
| `avgChatDurationMs`     | _number_                                                         | :heavy_check_mark: | Average chat request duration in milliseconds          |
| `avgTokensPerRequest`   | _number_                                                         | :heavy_check_mark: | Average tokens per chat request                        |
| `avgToolDurationMs`     | _number_                                                         | :heavy_check_mark: | Average tool call duration in milliseconds             |
| `distinctModels`        | _number_                                                         | :heavy_check_mark: | Number of distinct models used (project scope only)    |
| `distinctProviders`     | _number_                                                         | :heavy_check_mark: | Number of distinct providers used (project scope only) |
| `finishReasonStop`      | _number_                                                         | :heavy_check_mark: | Requests that completed naturally                      |
| `finishReasonToolCalls` | _number_                                                         | :heavy_check_mark: | Requests that resulted in tool calls                   |
| `firstSeenUnixNano`     | _string_                                                         | :heavy_check_mark: | Earliest activity timestamp in Unix nanoseconds        |
| `lastSeenUnixNano`      | _string_                                                         | :heavy_check_mark: | Latest activity timestamp in Unix nanoseconds          |
| `models`                | [components.ModelUsage](../../models/components/modelusage.md)[] | :heavy_check_mark: | List of models used with call counts                   |
| `toolCallFailure`       | _number_                                                         | :heavy_check_mark: | Failed tool calls (4xx/5xx status)                     |
| `toolCallSuccess`       | _number_                                                         | :heavy_check_mark: | Successful tool calls (2xx status)                     |
| `tools`                 | [components.ToolUsage](../../models/components/toolusage.md)[]   | :heavy_check_mark: | List of tools used with success/failure counts         |
| `totalChatRequests`     | _number_                                                         | :heavy_check_mark: | Total number of chat requests                          |
| `totalChats`            | _number_                                                         | :heavy_check_mark: | Number of unique chat sessions (project scope only)    |
| `totalInputTokens`      | _number_                                                         | :heavy_check_mark: | Sum of input tokens used                               |
| `totalOutputTokens`     | _number_                                                         | :heavy_check_mark: | Sum of output tokens used                              |
| `totalTokens`           | _number_                                                         | :heavy_check_mark: | Sum of all tokens used                                 |
| `totalToolCalls`        | _number_                                                         | :heavy_check_mark: | Total number of tool calls                             |
