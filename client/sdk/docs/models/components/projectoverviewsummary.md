# ProjectOverviewSummary

Aggregated project-level summary metrics for a time period

## Example Usage

```typescript
import { ProjectOverviewSummary } from "@gram/client/models/components/projectoverviewsummary.js";

let value: ProjectOverviewSummary = {
  activeServersCount: 261230,
  activeUsersCount: 859465,
  failedChats: 737684,
  failedToolCalls: 194984,
  llmClientBreakdown: [
    {
      activityCount: 992323,
      clientName: "<value>",
    },
  ],
  resolvedChats: 604321,
  topServers: [],
  topUsers: [],
  totalChats: 530793,
  totalToolCalls: 975819,
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `activeServersCount`                                                             | *number*                                                                         | :heavy_check_mark:                                                               | Number of MCP servers with at least one tool call in the time period             |
| `activeUsersCount`                                                               | *number*                                                                         | :heavy_check_mark:                                                               | Number of unique users with activity in the time period                          |
| `failedChats`                                                                    | *number*                                                                         | :heavy_check_mark:                                                               | Number of failed chat sessions                                                   |
| `failedToolCalls`                                                                | *number*                                                                         | :heavy_check_mark:                                                               | Number of failed tool calls                                                      |
| `llmClientBreakdown`                                                             | [components.LLMClientUsage](../../models/components/llmclientusage.md)[]         | :heavy_check_mark:                                                               | Breakdown of messages/activity by LLM client/agent                               |
| `resolvedChats`                                                                  | *number*                                                                         | :heavy_check_mark:                                                               | Number of resolved chat sessions                                                 |
| `topServers`                                                                     | [components.TopServer](../../models/components/topserver.md)[]                   | :heavy_check_mark:                                                               | Top 10 MCP servers by tool call count                                            |
| `topUsers`                                                                       | [components.TopUser](../../models/components/topuser.md)[]                       | :heavy_check_mark:                                                               | Top 10 users by activity (# of messages or tool calls depending on metrics_mode) |
| `totalChats`                                                                     | *number*                                                                         | :heavy_check_mark:                                                               | Total number of chat sessions                                                    |
| `totalToolCalls`                                                                 | *number*                                                                         | :heavy_check_mark:                                                               | Total number of tool calls                                                       |