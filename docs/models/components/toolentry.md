# ToolEntry

## Example Usage

```typescript
import { ToolEntry } from "@gram/client/models/components";

let value: ToolEntry = {
  id: "<id>",
  name: "<value>",
  toolUrn: "<value>",
  type: "externalmcp",
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `id`                                                                 | *string*                                                             | :heavy_check_mark:                                                   | The ID of the tool                                                   |
| `name`                                                               | *string*                                                             | :heavy_check_mark:                                                   | The name of the tool                                                 |
| `toolUrn`                                                            | *string*                                                             | :heavy_check_mark:                                                   | The URN of the tool                                                  |
| `type`                                                               | [components.ToolEntryType](../../models/components/toolentrytype.md) | :heavy_check_mark:                                                   | N/A                                                                  |