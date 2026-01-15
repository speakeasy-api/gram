# ToolChanges

Summary of tool changes between published and current versions

## Example Usage

```typescript
import { ToolChanges } from "@gram/client/models/components";

let value: ToolChanges = {
  added: [
    "<value 1>",
    "<value 2>",
    "<value 3>",
  ],
  addedCount: 815199,
  removed: [
    "<value 1>",
    "<value 2>",
    "<value 3>",
  ],
  removedCount: 793281,
};
```

## Fields

| Field                       | Type                        | Required                    | Description                 |
| --------------------------- | --------------------------- | --------------------------- | --------------------------- |
| `added`                     | *string*[]                  | :heavy_check_mark:          | Tool URNs that were added   |
| `addedCount`                | *number*                    | :heavy_check_mark:          | Number of tools added       |
| `removed`                   | *string*[]                  | :heavy_check_mark:          | Tool URNs that were removed |
| `removedCount`              | *number*                    | :heavy_check_mark:          | Number of tools removed     |