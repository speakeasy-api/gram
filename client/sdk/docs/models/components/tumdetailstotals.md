# TumDetailsTotals

Whole-range totals for the billing usage details. Distinct counts (sessions, active users) are computed over the full range and cannot be derived by summing the daily points.

## Example Usage

```typescript
import { TumDetailsTotals } from "@gram/client/models/components/tumdetailstotals.js";

let value: TumDetailsTotals = {
  activeUsers: 93671,
  agentSessions: 354372,
  cacheReadTokens: 720278,
  cacheWriteTokens: 665178,
  inputTokens: 338813,
  mcpToolTokens: 944482,
  outputTokens: 141365,
  riskyMessageTokens: 834672,
  skillTokens: 120604,
  toolCalls: 281971,
  toolMessageTokens: 698728,
  totalTokens: 11068,
  unattributedTokens: 289800,
};
```

## Fields

| Field                                                        | Type                                                         | Required                                                     | Description                                                  |
| ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `activeUsers`                                                | *number*                                                     | :heavy_check_mark:                                           | Distinct attributed users with usage                         |
| `agentSessions`                                              | *number*                                                     | :heavy_check_mark:                                           | Distinct chat sessions                                       |
| `cacheReadTokens`                                            | *number*                                                     | :heavy_check_mark:                                           | Cache read input tokens                                      |
| `cacheWriteTokens`                                           | *number*                                                     | :heavy_check_mark:                                           | Cache creation input tokens                                  |
| `inputTokens`                                                | *number*                                                     | :heavy_check_mark:                                           | Input tokens                                                 |
| `mcpToolTokens`                                              | *number*                                                     | :heavy_check_mark:                                           | Tokens attributed to MCP tool usage                          |
| `outputTokens`                                               | *number*                                                     | :heavy_check_mark:                                           | Output tokens                                                |
| `riskyMessageTokens`                                         | *number*                                                     | :heavy_check_mark:                                           | Tokens in messages carrying at least one active risk finding |
| `skillTokens`                                                | *number*                                                     | :heavy_check_mark:                                           | Tokens attributed to skill usage                             |
| `toolCalls`                                                  | *number*                                                     | :heavy_check_mark:                                           | Completed tool calls                                         |
| `toolMessageTokens`                                          | *number*                                                     | :heavy_check_mark:                                           | Tokens in tool-call messages                                 |
| `totalTokens`                                                | *number*                                                     | :heavy_check_mark:                                           | All tokens                                                   |
| `unattributedTokens`                                         | *number*                                                     | :heavy_check_mark:                                           | Tokens without user attribution                              |