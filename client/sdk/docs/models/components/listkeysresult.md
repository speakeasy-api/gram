# ListKeysResult

## Example Usage

```typescript
import { ListKeysResult } from "@gram/client/models/components";

let value: ListKeysResult = {
  keys: [
    {
      createdAt: new Date("2024-09-23T11:14:33.088Z"),
      createdByUserId: "<id>",
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      scopes: [
        "<value>",
      ],
      token: "<value>",
      updatedAt: new Date("2024-10-10T21:04:15.457Z"),
    },
  ],
};
```

## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `keys`                                             | [components.Key](../../models/components/key.md)[] | :heavy_check_mark:                                 | N/A                                                |