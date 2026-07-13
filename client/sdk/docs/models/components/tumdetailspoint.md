# TumDetailsPoint

One UTC day of billing usage details

## Example Usage

```typescript
import { TumDetailsPoint } from "@gram/client/models/components/tumdetailspoint.js";

let value: TumDetailsPoint = {
  activeUsers: 325006,
  agentSessions: 756255,
  bucketTimeUnixNano: "<value>",
  cacheReadTokens: 898036,
  cacheWriteTokens: 876148,
  inputTokens: 596335,
  mcpToolTokens: 760512,
  outputTokens: 574646,
  riskyMessageTokens: 944788,
  skillTokens: 151951,
  toolCalls: 780555,
  toolMessageTokens: 921880,
  totalTokens: 497571,
  unattributedTokens: 700995,
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `activeUsers`                                                   | *number*                                                        | :heavy_check_mark:                                              | Distinct attributed users with usage                            |
| `agentSessions`                                                 | *number*                                                        | :heavy_check_mark:                                              | Distinct chat sessions                                          |
| `bucketTimeUnixNano`                                            | *string*                                                        | :heavy_check_mark:                                              | Bucket start time in Unix nanoseconds (string for JS precision) |
| `cacheReadTokens`                                               | *number*                                                        | :heavy_check_mark:                                              | Cache read input tokens                                         |
| `cacheWriteTokens`                                              | *number*                                                        | :heavy_check_mark:                                              | Cache creation input tokens                                     |
| `inputTokens`                                                   | *number*                                                        | :heavy_check_mark:                                              | Input tokens                                                    |
| `mcpToolTokens`                                                 | *number*                                                        | :heavy_check_mark:                                              | Tokens attributed to MCP tool usage                             |
| `outputTokens`                                                  | *number*                                                        | :heavy_check_mark:                                              | Output tokens                                                   |
| `riskyMessageTokens`                                            | *number*                                                        | :heavy_check_mark:                                              | Tokens in messages carrying at least one active risk finding    |
| `skillTokens`                                                   | *number*                                                        | :heavy_check_mark:                                              | Tokens attributed to skill usage                                |
| `toolCalls`                                                     | *number*                                                        | :heavy_check_mark:                                              | Completed tool calls                                            |
| `toolMessageTokens`                                             | *number*                                                        | :heavy_check_mark:                                              | Tokens in tool-call messages                                    |
| `totalTokens`                                                   | *number*                                                        | :heavy_check_mark:                                              | All tokens                                                      |
| `unattributedTokens`                                            | *number*                                                        | :heavy_check_mark:                                              | Tokens without user attribution                                 |