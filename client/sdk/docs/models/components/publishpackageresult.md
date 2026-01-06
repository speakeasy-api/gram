# PublishPackageResult

## Example Usage

```typescript
import { PublishPackageResult } from "@gram/client/models/components";

let value: PublishPackageResult = {
  package: {
    createdAt: new Date("2026-04-29T19:32:19.315Z"),
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    updatedAt: new Date("2024-02-03T19:41:53.384Z"),
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