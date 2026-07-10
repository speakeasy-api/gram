# GetProjectOverviewResult

Result of project overview query

## Example Usage

```typescript
import { GetProjectOverviewResult } from "@gram/client/models/components/getprojectoverviewresult.js";

let value: GetProjectOverviewResult = {
  comparison: {
    activeServersCount: 607224,
    activeUsersCount: 9452,
    failedChats: 482365,
    failedToolCalls: 114640,
    llmClientBreakdown: [],
    resolvedChats: 403873,
    topServers: [
      {
        serverName: "<value>",
        toolCallCount: 690654,
      },
    ],
    topUsers: [
      {
        activityCount: 245916,
        userId: "<id>",
        userType: "external",
      },
    ],
    totalChats: 410058,
    totalToolCalls: 113180,
  },
  metricsMode: "session",
  summary: {
    activeServersCount: 338922,
    activeUsersCount: 244035,
    failedChats: 171005,
    failedToolCalls: 691823,
    llmClientBreakdown: [
      {
        activityCount: 992323,
        clientName: "<value>",
      },
    ],
    resolvedChats: 731813,
    topServers: [],
    topUsers: [
      {
        activityCount: 245916,
        userId: "<id>",
        userType: "external",
      },
    ],
    totalChats: 202910,
    totalToolCalls: 880065,
  },
};
```

## Fields

| Field         | Type                                                                                   | Required           | Description                                                    |
| ------------- | -------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------- |
| `comparison`  | [components.ProjectOverviewSummary](../../models/components/projectoverviewsummary.md) | :heavy_check_mark: | Aggregated project-level summary metrics for a time period     |
| `metricsMode` | [components.MetricsMode](../../models/components/metricsmode.md)                       | :heavy_check_mark: | Indicates whether metrics are session-based or tool-call-based |
| `summary`     | [components.ProjectOverviewSummary](../../models/components/projectoverviewsummary.md) | :heavy_check_mark: | Aggregated project-level summary metrics for a time period     |
