# Integration

## Example Usage

```typescript
import { Integration } from "@gram/client/models/components";

let value: Integration = {
  packageId: "<id>",
  packageName: "<value>",
  packageSummary: "<value>",
  packageTitle: "<value>",
  toolNames: [
    "<value 1>",
    "<value 2>",
  ],
  version: "<value>",
  versionCreatedAt: new Date("2026-10-27T14:06:45.597Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `packageDescription`                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `packageDescriptionRaw`                                                                       | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `packageId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `packageImageAssetId`                                                                         | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `packageKeywords`                                                                             | *string*[]                                                                                    | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `packageName`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `packageSummary`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `packageTitle`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `packageUrl`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `toolNames`                                                                                   | *string*[]                                                                                    | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `version`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The latest version of the integration                                                         |
| `versionCreatedAt`                                                                            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `versions`                                                                                    | [components.IntegrationVersion](../../models/components/integrationversion.md)[]              | :heavy_minus_sign:                                                                            | N/A                                                                                           |