# ToolUsageUsersByTargetRow

Aggregated tool usage metrics for one target and user identity

## Example Usage

```typescript
import { ToolUsageUsersByTargetRow } from "@gram/client/models/components/toolusageusersbytargetrow.js";

let value: ToolUsageUsersByTargetRow = {
  eventCount: 434310,
  failureCount: 340870,
  targetId: "<id>",
  targetKind: "local_tools",
  targetLabel: "<value>",
  targetType: "tunneled_mcp_server",
  userKey: "<value>",
  userKind: "user_id",
  userLabel: "<value>",
};
```

## Fields

| Field                                                                                                            | Type                                                                                                             | Required                                                                                                         | Description                                                                                                      |
| ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `eventCount`                                                                                                     | *number*                                                                                                         | :heavy_check_mark:                                                                                               | Total number of tool usage events for the target and user identity                                               |
| `failureCount`                                                                                                   | *number*                                                                                                         | :heavy_check_mark:                                                                                               | Number of failed tool usage events for the target and user identity                                              |
| `targetId`                                                                                                       | *string*                                                                                                         | :heavy_check_mark:                                                                                               | Stable target identifier used by filters and chart grouping                                                      |
| `targetKind`                                                                                                     | [components.ToolUsageUsersByTargetRowTargetKind](../../models/components/toolusageusersbytargetrowtargetkind.md) | :heavy_check_mark:                                                                                               | Tool usage aggregation target kind                                                                               |
| `targetLabel`                                                                                                    | *string*                                                                                                         | :heavy_check_mark:                                                                                               | User-facing label for the target                                                                                 |
| `targetType`                                                                                                     | [components.ToolUsageUsersByTargetRowTargetType](../../models/components/toolusageusersbytargetrowtargettype.md) | :heavy_check_mark:                                                                                               | Tool usage target type                                                                                           |
| `userKey`                                                                                                        | *string*                                                                                                         | :heavy_check_mark:                                                                                               | Stable user identity value used by filters and chart grouping                                                    |
| `userKind`                                                                                                       | [components.ToolUsageUsersByTargetRowUserKind](../../models/components/toolusageusersbytargetrowuserkind.md)     | :heavy_check_mark:                                                                                               | Tool usage user identity kind                                                                                    |
| `userLabel`                                                                                                      | *string*                                                                                                         | :heavy_check_mark:                                                                                               | User-facing label for the user identity                                                                          |