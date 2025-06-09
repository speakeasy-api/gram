# CreatePackageResult

## Example Usage

```typescript
import { CreatePackageResult } from "@gram/client/models/components";

let value: CreatePackageResult = {
  package: {
    createdAt: new Date("2024-12-22T16:38:24.381Z"),
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    updatedAt: new Date("2023-05-30T03:03:42.835Z"),
  },
};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `package`                                                | [components.Package](../../models/components/package.md) | :heavy_check_mark:                                       | N/A                                                      |