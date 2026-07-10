# ToolUsageUserSummary

Aggregated tool usage metrics for one user identity

## Example Usage

```typescript
import { ToolUsageUserSummary } from "@gram/client/models/components/toolusageusersummary.js";

let value: ToolUsageUserSummary = {
  eventCount: 182422,
  failureCount: 376408,
  failureRate: 4433.93,
  successCount: 341971,
  uniqueTools: 57140,
  userKey: "<value>",
  userKind: "email",
  userLabel: "<value>",
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `eventCount`                                                                                       | *number*                                                                                           | :heavy_check_mark:                                                                                 | Total number of tool usage events for the user identity                                            |
| `failureCount`                                                                                     | *number*                                                                                           | :heavy_check_mark:                                                                                 | Number of failed tool usage events for the user identity                                           |
| `failureRate`                                                                                      | *number*                                                                                           | :heavy_check_mark:                                                                                 | Fraction of completed tool usage events for the user identity that failed                          |
| `successCount`                                                                                     | *number*                                                                                           | :heavy_check_mark:                                                                                 | Number of successful tool usage events for the user identity                                       |
| `uniqueTools`                                                                                      | *number*                                                                                           | :heavy_check_mark:                                                                                 | Number of distinct tools observed for the user identity                                            |
| `userKey`                                                                                          | *string*                                                                                           | :heavy_check_mark:                                                                                 | Stable user identity value used by filters and chart grouping                                      |
| `userKind`                                                                                         | [components.ToolUsageUserSummaryUserKind](../../models/components/toolusageusersummaryuserkind.md) | :heavy_check_mark:                                                                                 | Tool usage user identity kind                                                                      |
| `userLabel`                                                                                        | *string*                                                                                           | :heavy_check_mark:                                                                                 | User-facing label for the user identity                                                            |