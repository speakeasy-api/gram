# ListPackagesResult

## Example Usage

```typescript
import { ListPackagesResult } from "@gram/client/models/components";

let value: ListPackagesResult = {
  packages: [
    {
      createdAt: new Date("2024-02-21T04:00:43.531Z"),
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      updatedAt: new Date("2024-11-17T17:23:14.895Z"),
    },
  ],
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `packages`                                                 | [components.Package](../../models/components/package.md)[] | :heavy_check_mark:                                         | The list of packages                                       |