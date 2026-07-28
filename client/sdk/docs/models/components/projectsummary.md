# ProjectSummary

Aggregated metrics

## Example Usage

```typescript
import { ProjectSummary } from "@gram/client/models/components/projectsummary.js";

let value: ProjectSummary = {
  avgChatDurationMs: 1842.11,
  avgChatResolutionScore: 7428.13,
  avgTokensPerRequest: 9336.93,
  avgToolDurationMs: 3292.98,
  cacheCreationInputTokens: 28356,
  cacheReadInputTokens: 899256,
  chatResolutionAbandoned: 722052,
  chatResolutionFailure: 722032,
  chatResolutionPartial: 744108,
  chatResolutionSuccess: 788343,
  distinctModels: 193192,
  distinctProviders: 875206,
  finishReasonStop: 979289,
  finishReasonToolCalls: 225387,
  firstSeenUnixNano: "<value>",
  lastSeenUnixNano: "<value>",
  models: [
    {
      count: 121591,
      name: "<value>",
    },
  ],
  toolCallFailure: 200879,
  toolCallSuccess: 962111,
  tools: [],
  totalChatRequests: 258376,
  totalChats: 656311,
  totalCost: 7212.5,
  totalInputTokens: 380118,
  totalOutputTokens: 944162,
  totalTokens: 542205,
  totalToolCalls: 838349,
};
```

## Fields

| Field                      | Type                                                             | Required           | Description                                            |
| -------------------------- | ---------------------------------------------------------------- | ------------------ | ------------------------------------------------------ |
| `avgChatDurationMs`        | _number_                                                         | :heavy_check_mark: | Average chat request duration in milliseconds          |
| `avgChatResolutionScore`   | _number_                                                         | :heavy_check_mark: | Average chat resolution score (0-100)                  |
| `avgTokensPerRequest`      | _number_                                                         | :heavy_check_mark: | Average tokens per chat request                        |
| `avgToolDurationMs`        | _number_                                                         | :heavy_check_mark: | Average tool call duration in milliseconds             |
| `cacheCreationInputTokens` | _number_                                                         | :heavy_check_mark: | Sum of cache creation input tokens                     |
| `cacheReadInputTokens`     | _number_                                                         | :heavy_check_mark: | Sum of cache read input tokens                         |
| `chatResolutionAbandoned`  | _number_                                                         | :heavy_check_mark: | Chats abandoned by user                                |
| `chatResolutionFailure`    | _number_                                                         | :heavy_check_mark: | Chats that failed to resolve                           |
| `chatResolutionPartial`    | _number_                                                         | :heavy_check_mark: | Chats partially resolved                               |
| `chatResolutionSuccess`    | _number_                                                         | :heavy_check_mark: | Chats resolved successfully                            |
| `distinctModels`           | _number_                                                         | :heavy_check_mark: | Number of distinct models used (project scope only)    |
| `distinctProviders`        | _number_                                                         | :heavy_check_mark: | Number of distinct providers used (project scope only) |
| `finishReasonStop`         | _number_                                                         | :heavy_check_mark: | Requests that completed naturally                      |
| `finishReasonToolCalls`    | _number_                                                         | :heavy_check_mark: | Requests that resulted in tool calls                   |
| `firstSeenUnixNano`        | _string_                                                         | :heavy_check_mark: | Earliest activity timestamp in Unix nanoseconds        |
| `lastSeenUnixNano`         | _string_                                                         | :heavy_check_mark: | Latest activity timestamp in Unix nanoseconds          |
| `models`                   | [components.ModelUsage](../../models/components/modelusage.md)[] | :heavy_check_mark: | List of models used with call counts                   |
| `toolCallFailure`          | _number_                                                         | :heavy_check_mark: | Failed tool calls (4xx/5xx status)                     |
| `toolCallSuccess`          | _number_                                                         | :heavy_check_mark: | Successful tool calls (2xx status)                     |
| `tools`                    | [components.ToolUsage](../../models/components/toolusage.md)[]   | :heavy_check_mark: | List of tools used with success/failure counts         |
| `totalChatRequests`        | _number_                                                         | :heavy_check_mark: | Total number of chat requests                          |
| `totalChats`               | _number_                                                         | :heavy_check_mark: | Number of unique chat sessions (project scope only)    |
| `totalCost`                | _number_                                                         | :heavy_check_mark: | Total cost of all requests                             |
| `totalInputTokens`         | _number_                                                         | :heavy_check_mark: | Sum of input tokens used                               |
| `totalOutputTokens`        | _number_                                                         | :heavy_check_mark: | Sum of output tokens used                              |
| `totalTokens`              | _number_                                                         | :heavy_check_mark: | Sum of all tokens used                                 |
| `totalToolCalls`           | _number_                                                         | :heavy_check_mark: | Total number of tool calls                             |
