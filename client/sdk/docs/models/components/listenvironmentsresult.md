# ListEnvironmentsResult

Result type for listing environments

## Example Usage

```typescript
import { ListEnvironmentsResult } from "@gram/client/models/components";

let value: ListEnvironmentsResult = {
  environments: [
    {
      createdAt: new Date("2025-04-28T13:26:34.681Z"),
      entries: [
        {
          createdAt: new Date("2024-05-14T22:34:42.019Z"),
          name: "<value>",
          updatedAt: new Date("2024-09-15T00:05:11.728Z"),
          value: "<value>",
        },
      ],
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      slug: "<value>",
      updatedAt: new Date("2023-01-21T14:14:48.878Z"),
    },
  ],
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `environments`                                                     | [components.Environment](../../models/components/environment.md)[] | :heavy_check_mark:                                                 | N/A                                                                |