# ListVersionsResult

## Example Usage

```typescript
import { ListVersionsResult } from "@gram/client/models/components/listversionsresult.js";

let value: ListVersionsResult = {
  package: {
    createdAt: new Date("2025-12-22T16:38:24.381Z"),
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    updatedAt: new Date("2024-05-29T03:03:42.835Z"),
  },
  versions: [],
};
```

## Fields

| Field      | Type                                                                     | Required           | Description |
| ---------- | ------------------------------------------------------------------------ | ------------------ | ----------- |
| `package`  | [components.Package](../../models/components/package.md)                 | :heavy_check_mark: | N/A         |
| `versions` | [components.PackageVersion](../../models/components/packageversion.md)[] | :heavy_check_mark: | N/A         |
