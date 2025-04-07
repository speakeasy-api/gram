# ListEnvironmentsResult

Result type for listing environments

## Example Usage

```typescript
import { ListEnvironmentsResult } from "@gram/sdk/models/components";

let value: ListEnvironmentsResult = {
  environments: [
    {
      createdAt: new Date("2025-02-25T11:58:58.144Z"),
      entries: [
        {
          createdAt: new Date("2024-09-17T23:46:52.488Z"),
          name: "<value>",
          updatedAt: new Date("2025-12-31T18:49:14.472Z"),
          value: "<value>",
        },
      ],
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      slug: "<value>",
      updatedAt: new Date("2025-04-04T09:14:28.012Z"),
    },
  ],
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `environments`                                                     | [components.Environment](../../models/components/environment.md)[] | :heavy_check_mark:                                                 | N/A                                                                |