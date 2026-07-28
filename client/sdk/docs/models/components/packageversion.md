# PackageVersion

## Example Usage

```typescript
import { PackageVersion } from "@gram/client/models/components/packageversion.js";

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

| Field          | Type                                                                                          | Required           | Description                                          |
| -------------- | --------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------- |
| `createdAt`    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The creation date of the package version             |
| `deploymentId` | _string_                                                                                      | :heavy_check_mark: | The ID of the deployment that the version belongs to |
| `id`           | _string_                                                                                      | :heavy_check_mark: | The ID of the package version                        |
| `packageId`    | _string_                                                                                      | :heavy_check_mark: | The ID of the package that the version belongs to    |
| `semver`       | _string_                                                                                      | :heavy_check_mark: | The semantic version value                           |
| `visibility`   | _string_                                                                                      | :heavy_check_mark: | The visibility of the package version                |
