# UpdatePackageResult

## Example Usage

```typescript
import { UpdatePackageResult } from "@gram/client/models/components";

let value: UpdatePackageResult = {
  package: {
    createdAt: new Date("2024-06-06T09:18:15.708Z"),
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    updatedAt: new Date("2023-08-25T11:16:01.596Z"),
  },
};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `package`                                                | [components.Package](../../models/components/package.md) | :heavy_check_mark:                                       | N/A                                                      |