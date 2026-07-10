# Package

## Example Usage

```typescript
import { Package } from "@gram/client/models/components/package.js";

let value: Package = {
  createdAt: new Date("2025-02-09T19:08:11.368Z"),
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  projectId: "<id>",
  updatedAt: new Date("2025-01-08T01:15:25.788Z"),
};
```

## Fields

| Field            | Type                                                                                          | Required           | Description                                                                                      |
| ---------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------ |
| `createdAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The creation date of the package                                                                 |
| `deletedAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | The deletion date of the package                                                                 |
| `description`    | _string_                                                                                      | :heavy_minus_sign: | The description of the package. This contains HTML content.                                      |
| `descriptionRaw` | _string_                                                                                      | :heavy_minus_sign: | The unsanitized, user-supplied description of the package. Limited markdown syntax is supported. |
| `id`             | _string_                                                                                      | :heavy_check_mark: | The ID of the package                                                                            |
| `imageAssetId`   | _string_                                                                                      | :heavy_minus_sign: | The asset ID of the image to show for this package                                               |
| `keywords`       | _string_[]                                                                                    | :heavy_minus_sign: | The keywords of the package                                                                      |
| `latestVersion`  | _string_                                                                                      | :heavy_minus_sign: | The latest version of the package                                                                |
| `name`           | _string_                                                                                      | :heavy_check_mark: | The name of the package                                                                          |
| `organizationId` | _string_                                                                                      | :heavy_check_mark: | The ID of the organization that owns the package                                                 |
| `projectId`      | _string_                                                                                      | :heavy_check_mark: | The ID of the project that owns the package                                                      |
| `summary`        | _string_                                                                                      | :heavy_minus_sign: | The summary of the package                                                                       |
| `title`          | _string_                                                                                      | :heavy_minus_sign: | The title of the package                                                                         |
| `updatedAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The last update date of the package                                                              |
| `url`            | _string_                                                                                      | :heavy_minus_sign: | External URL for the package owner                                                               |
