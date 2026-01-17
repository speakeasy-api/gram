# ResourceChanges

Summary of resource changes between published and current versions

## Example Usage

```typescript
import { ResourceChanges } from "@gram/client/models/components";

let value: ResourceChanges = {
  added: [],
  addedCount: 700973,
  removed: [
    "<value 1>",
    "<value 2>",
    "<value 3>",
  ],
  removedCount: 272738,
};
```

## Fields

| Field                           | Type                            | Required                        | Description                     |
| ------------------------------- | ------------------------------- | ------------------------------- | ------------------------------- |
| `added`                         | *string*[]                      | :heavy_check_mark:              | Resource URNs that were added   |
| `addedCount`                    | *number*                        | :heavy_check_mark:              | Number of resources added       |
| `removed`                       | *string*[]                      | :heavy_check_mark:              | Resource URNs that were removed |
| `removedCount`                  | *number*                        | :heavy_check_mark:              | Number of resources removed     |