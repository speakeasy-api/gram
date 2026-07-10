# UserSummary

Aggregated usage summary for a single user

## Example Usage

```typescript
import { UserSummary } from "@gram/client/models/components/usersummary.js";

let value: UserSummary = {
  avgTokensPerRequest: 9405.09,
  cacheCreationInputTokens: 206020,
  cacheReadInputTokens: 597002,
  firstSeenUnixNano: "<value>",
  hookSources: [],
  lastSeenUnixNano: "<value>",
  toolCallFailure: 693595,
  toolCallSuccess: 94181,
  tools: [],
  totalChatRequests: 224071,
  totalChats: 930365,
  totalCost: 9133.72,
  totalInputTokens: 289044,
  totalOutputTokens: 310647,
  totalTokens: 390182,
  totalToolCalls: 80590,
  userEmail: "<value>",
  userId: "<id>",
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `accountTypes`                                                             | *string*[]                                                                 | :heavy_minus_sign:                                                         | Distinct account types observed for this user ('team', 'personal')         |
| `accounts`                                                                 | [components.UserAccount](../../models/components/useraccount.md)[]         | :heavy_minus_sign:                                                         | Linked AI accounts for this user (team and personal, across providers)     |
| `avgTokensPerRequest`                                                      | *number*                                                                   | :heavy_check_mark:                                                         | Average tokens per chat request                                            |
| `cacheCreationInputTokens`                                                 | *number*                                                                   | :heavy_check_mark:                                                         | Sum of cache creation input tokens                                         |
| `cacheReadInputTokens`                                                     | *number*                                                                   | :heavy_check_mark:                                                         | Sum of cache read input tokens                                             |
| `firstSeenUnixNano`                                                        | *string*                                                                   | :heavy_check_mark:                                                         | Earliest activity timestamp in Unix nanoseconds                            |
| `hookSources`                                                              | [components.HookSourceUsage](../../models/components/hooksourceusage.md)[] | :heavy_check_mark:                                                         | Per-hook-source usage breakdown                                            |
| `lastSeenUnixNano`                                                         | *string*                                                                   | :heavy_check_mark:                                                         | Latest activity timestamp in Unix nanoseconds                              |
| `toolCallFailure`                                                          | *number*                                                                   | :heavy_check_mark:                                                         | Failed tool calls (4xx/5xx status)                                         |
| `toolCallSuccess`                                                          | *number*                                                                   | :heavy_check_mark:                                                         | Successful tool calls (2xx status)                                         |
| `tools`                                                                    | [components.ToolUsage](../../models/components/toolusage.md)[]             | :heavy_check_mark:                                                         | Per-tool usage breakdown                                                   |
| `totalChatRequests`                                                        | *number*                                                                   | :heavy_check_mark:                                                         | Total number of chat completion requests                                   |
| `totalChats`                                                               | *number*                                                                   | :heavy_check_mark:                                                         | Number of unique chat sessions                                             |
| `totalCost`                                                                | *number*                                                                   | :heavy_check_mark:                                                         | Total cost of all requests                                                 |
| `totalInputTokens`                                                         | *number*                                                                   | :heavy_check_mark:                                                         | Sum of input tokens used                                                   |
| `totalOutputTokens`                                                        | *number*                                                                   | :heavy_check_mark:                                                         | Sum of output tokens used                                                  |
| `totalTokens`                                                              | *number*                                                                   | :heavy_check_mark:                                                         | Sum of all tokens used                                                     |
| `totalToolCalls`                                                           | *number*                                                                   | :heavy_check_mark:                                                         | Total number of tool calls                                                 |
| `userEmail`                                                                | *string*                                                                   | :heavy_check_mark:                                                         | User email associated with this usage, when present                        |
| `userId`                                                                   | *string*                                                                   | :heavy_check_mark:                                                         | User identifier (user_id or external_user_id depending on group_by)        |