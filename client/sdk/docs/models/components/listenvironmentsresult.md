# ListEnvironmentsResult

Result type for listing environments

## Example Usage

```typescript
import { ListEnvironmentsResult } from "@gram/client/models/components";

let value: ListEnvironmentsResult = {
  environments: [
    {
      createdAt: new Date("2025-09-17T23:46:52.488Z"),
      entries: [
        {
          createdAt: new Date("2026-04-04T09:14:28.012Z"),
          name: "<value>",
          updatedAt: new Date("2025-02-24T05:54:42.792Z"),
          value: "<value>",
        },
      ],
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      slug: "<value>",
      updatedAt: new Date("2026-02-10T18:18:16.412Z"),
    },
  ],
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `environments`                                                     | [components.Environment](../../models/components/environment.md)[] | :heavy_check_mark:                                                 | N/A                                                                |