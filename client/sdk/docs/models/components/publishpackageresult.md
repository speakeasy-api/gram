# PublishPackageResult

## Example Usage

```typescript
import { PublishPackageResult } from "@gram/client/models/components/publishpackageresult.js";

let value: PublishPackageResult = {
  package: {
    createdAt: new Date("2025-12-22T16:38:24.381Z"),
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    updatedAt: new Date("2024-05-29T03:03:42.835Z"),
  },
  version: {
    createdAt: new Date("2024-03-11T09:33:18.947Z"),
    deploymentId: "<id>",
    id: "<id>",
    packageId: "<id>",
    semver: "<value>",
    visibility: "<value>",
  },
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `package`                                                              | [components.Package](../../models/components/package.md)               | :heavy_check_mark:                                                     | N/A                                                                    |
| `version`                                                              | [components.PackageVersion](../../models/components/packageversion.md) | :heavy_check_mark:                                                     | N/A                                                                    |