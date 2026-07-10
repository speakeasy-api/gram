# ToolUsageUserFilter

Typed user identity filter

## Example Usage

```typescript
import { ToolUsageUserFilter } from "@gram/client/models/components/toolusageuserfilter.js";

let value: ToolUsageUserFilter = {
  key: "<key>",
  kind: "unknown",
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `key`                                                                                    | *string*                                                                                 | :heavy_check_mark:                                                                       | User identity value to include                                                           |
| `kind`                                                                                   | [components.ToolUsageUserFilterKind](../../models/components/toolusageuserfilterkind.md) | :heavy_check_mark:                                                                       | Tool usage user identity kind                                                            |