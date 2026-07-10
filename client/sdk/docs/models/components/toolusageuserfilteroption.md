# ToolUsageUserFilterOption

Tool usage user filter option with usage in the selected time window

## Example Usage

```typescript
import { ToolUsageUserFilterOption } from "@gram/client/models/components/toolusageuserfilteroption.js";

let value: ToolUsageUserFilterOption = {
  eventCount: 254803,
  userKey: "<value>",
  userKind: "email",
  userLabel: "<value>",
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `eventCount`                                               | *number*                                                   | :heavy_check_mark:                                         | Number of tool usage events observed for the user identity |
| `userKey`                                                  | *string*                                                   | :heavy_check_mark:                                         | Stable user identity value used by filters                 |
| `userKind`                                                 | [components.UserKind](../../models/components/userkind.md) | :heavy_check_mark:                                         | Tool usage user identity kind                              |
| `userLabel`                                                | *string*                                                   | :heavy_check_mark:                                         | User-facing label for the user identity                    |