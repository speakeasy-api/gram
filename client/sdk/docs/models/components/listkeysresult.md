# ListKeysResult

## Example Usage

```typescript
import { ListKeysResult } from "@gram/sdk/models/components";

let value: ListKeysResult = {
  keys: [
    {
      createdAt: new Date("2024-11-01T20:33:57.206Z"),
      createdByUserId: "<id>",
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      scopes: [
        "<value>",
      ],
      token: "<value>",
      updatedAt: new Date("2024-11-07T03:49:54.674Z"),
    },
  ],
};
```

## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `keys`                                             | [components.Key](../../models/components/key.md)[] | :heavy_check_mark:                                 | N/A                                                |