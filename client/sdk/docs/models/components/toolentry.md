# ToolEntry

## Example Usage

```typescript
import { ToolEntry } from "@gram/client/models/components";

let value: ToolEntry = {
  id: "<id>",
  name: "<value>",
  toolUrn: "<value>",
  type: "prompt",
};
```

## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `id`                                               | *string*                                           | :heavy_check_mark:                                 | The ID of the tool                                 |
| `name`                                             | *string*                                           | :heavy_check_mark:                                 | The name of the tool                               |
| `toolUrn`                                          | *string*                                           | :heavy_check_mark:                                 | The URN of the tool                                |
| `type`                                             | [components.Type](../../models/components/type.md) | :heavy_check_mark:                                 | N/A                                                |