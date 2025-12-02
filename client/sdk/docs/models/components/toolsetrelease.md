# ToolsetRelease

## Example Usage

```typescript
import { ToolsetRelease } from "@gram/client/models/components";

let value: ToolsetRelease = {
  createdAt: new Date("2024-11-23T11:20:52.066Z"),
  id: "<id>",
  releaseNumber: 906506,
  releasedByUserId: "<id>",
  toolsetId: "<id>",
  toolsetVersionId: "<id>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the release was created                                                                  |
| `globalVariationsVersionId`                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | The global variations version ID (optional)                                                   |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the release                                                                         |
| `notes`                                                                                       | *string*                                                                                      | :heavy_minus_sign:                                                                            | Release notes                                                                                 |
| `releaseNumber`                                                                               | *number*                                                                                      | :heavy_check_mark:                                                                            | The sequential release number                                                                 |
| `releasedByUserId`                                                                            | *string*                                                                                      | :heavy_check_mark:                                                                            | The user who created this release                                                             |
| `sourceStateId`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | The source state ID captured in this release                                                  |
| `toolsetId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The toolset this release belongs to                                                           |
| `toolsetVariationsVersionId`                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | The toolset-scoped variations version ID (optional)                                           |
| `toolsetVersionId`                                                                            | *string*                                                                                      | :heavy_check_mark:                                                                            | The toolset version ID captured in this release                                               |