# ListEnvironmentsResult

Result type for listing environments

## Example Usage

```typescript
import { ListEnvironmentsResult } from "@gram/sdk/models/components";

let value: ListEnvironmentsResult = {
  environments: [
    {
      createdAt: new Date("2023-10-17T22:52:14.955Z"),
      entries: [
        {
          createdAt: new Date("2025-04-28T13:26:34.681Z"),
          name: "<value>",
          updatedAt: new Date("2024-05-14T22:34:42.019Z"),
          value: "<value>",
        },
      ],
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      slug: "<value>",
      updatedAt: new Date("2024-09-15T00:05:11.728Z"),
    },
  ],
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `environments`                                                     | [components.Environment](../../models/components/environment.md)[] | :heavy_check_mark:                                                 | N/A                                                                |