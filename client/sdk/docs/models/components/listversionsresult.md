# ListVersionsResult

## Example Usage

```typescript
import { ListVersionsResult } from "@gram/client/models/components";

let value: ListVersionsResult = {
  package: {
    createdAt: new Date("2026-04-29T19:32:19.315Z"),
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    updatedAt: new Date("2024-02-03T19:41:53.384Z"),
  },
  versions: [],
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `package`                                                                | [components.Package](../../models/components/package.md)                 | :heavy_check_mark:                                                       | N/A                                                                      |
| `versions`                                                               | [components.PackageVersion](../../models/components/packageversion.md)[] | :heavy_check_mark:                                                       | N/A                                                                      |