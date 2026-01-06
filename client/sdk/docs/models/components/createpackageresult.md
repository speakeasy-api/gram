# CreatePackageResult

## Example Usage

```typescript
import { CreatePackageResult } from "@gram/client/models/components";

let value: CreatePackageResult = {
  package: {
    createdAt: new Date("2026-04-29T19:32:19.315Z"),
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    updatedAt: new Date("2024-02-03T19:41:53.384Z"),
  },
};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `package`                                                | [components.Package](../../models/components/package.md) | :heavy_check_mark:                                       | N/A                                                      |