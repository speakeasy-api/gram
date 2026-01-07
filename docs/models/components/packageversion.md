# PackageVersion

## Example Usage

```typescript
import { PackageVersion } from "@gram/client/models/components";

let value: PackageVersion = {
  createdAt: new Date("2026-06-08T08:06:08.495Z"),
  deploymentId: "<id>",
  id: "<id>",
  packageId: "<id>",
  semver: "<value>",
  visibility: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the package version                                                      |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the deployment that the version belongs to                                          |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the package version                                                                 |
| `packageId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the package that the version belongs to                                             |
| `semver`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | The semantic version value                                                                    |
| `visibility`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | The visibility of the package version                                                         |