# AllowedOrigin

## Example Usage

```typescript
import { AllowedOrigin } from "@gram/client/models/components";

let value: AllowedOrigin = {
  createdAt: new Date("2025-05-20T07:37:19.273Z"),
  id: "<id>",
  origin: "<value>",
  projectId: "<id>",
  updatedAt: new Date("2026-11-13T23:13:20.459Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the allowed origin.                                                      |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the allowed origin                                                                  |
| `origin`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | The origin URL                                                                                |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the project                                                                         |
| `status`                                                                                      | [components.AllowedOriginStatus](../../models/components/allowedoriginstatus.md)              | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the allowed origin.                                                   |