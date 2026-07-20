# Project

## Example Usage

```typescript
import { Project } from "@gram/client/models/components/project.js";

let value: Project = {
  createdAt: new Date("2026-06-12T00:25:18.992Z"),
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  slug: "<value>",
  updatedAt: new Date("2025-04-05T14:08:44.366Z"),
};
```

## Fields

| Field            | Type                                                                                          | Required           | Description                                                     |
| ---------------- | --------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------- |
| `createdAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The creation date of the project.                               |
| `id`             | _string_                                                                                      | :heavy_check_mark: | The ID of the project                                           |
| `logoAssetId`    | _string_                                                                                      | :heavy_minus_sign: | The ID of the logo asset for the project                        |
| `name`           | _string_                                                                                      | :heavy_check_mark: | The name of the project                                         |
| `organizationId` | _string_                                                                                      | :heavy_check_mark: | The ID of the organization that owns the project                |
| `slug`           | _string_                                                                                      | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource. |
| `updatedAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The last update date of the project.                            |
