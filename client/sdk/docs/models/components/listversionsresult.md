# ListVersionsResult

## Example Usage

```typescript
import { ListVersionsResult } from "@gram/client/models/components";

let value: ListVersionsResult = {
  package: {
    createdAt: new Date("2024-04-01T09:53:12.577Z"),
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    updatedAt: new Date("2023-10-24T00:26:29.194Z"),
  },
  versions: [
    {
      createdAt: new Date("2025-06-24T10:29:38.669Z"),
      deploymentId: "<id>",
      id: "<id>",
      packageId: "<id>",
      semver: "<value>",
      visibility: "<value>",
    },
  ],
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `package`                                                                | [components.Package](../../models/components/package.md)                 | :heavy_check_mark:                                                       | N/A                                                                      |
| `versions`                                                               | [components.PackageVersion](../../models/components/packageversion.md)[] | :heavy_check_mark:                                                       | N/A                                                                      |