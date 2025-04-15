# ListKeysResult

## Example Usage

```typescript
import { ListKeysResult } from "@gram/client/models/components";

let value: ListKeysResult = {
  keys: [
    {
      createdAt: new Date("2025-03-20T15:32:38.576Z"),
      createdByUserId: "<id>",
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      scopes: [
        "<value>",
      ],
      token: "<value>",
      updatedAt: new Date("2024-09-10T01:23:16.211Z"),
    },
  ],
};
```

## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `keys`                                             | [components.Key](../../models/components/key.md)[] | :heavy_check_mark:                                 | N/A                                                |