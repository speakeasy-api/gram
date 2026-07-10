# Integration

## Example Usage

```typescript
import { Integration } from "@gram/client/models/components/integration.js";

let value: Integration = {
  packageId: "<id>",
  packageName: "<value>",
  packageSummary: "<value>",
  packageTitle: "<value>",
  toolNames: ["<value 1>", "<value 2>"],
  version: "<value>",
  versionCreatedAt: new Date("2026-10-27T14:06:45.597Z"),
};
```

## Fields

| Field                   | Type                                                                                          | Required           | Description                           |
| ----------------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------- |
| `packageDescription`    | _string_                                                                                      | :heavy_minus_sign: | N/A                                   |
| `packageDescriptionRaw` | _string_                                                                                      | :heavy_minus_sign: | N/A                                   |
| `packageId`             | _string_                                                                                      | :heavy_check_mark: | N/A                                   |
| `packageImageAssetId`   | _string_                                                                                      | :heavy_minus_sign: | N/A                                   |
| `packageKeywords`       | _string_[]                                                                                    | :heavy_minus_sign: | N/A                                   |
| `packageName`           | _string_                                                                                      | :heavy_check_mark: | N/A                                   |
| `packageSummary`        | _string_                                                                                      | :heavy_check_mark: | N/A                                   |
| `packageTitle`          | _string_                                                                                      | :heavy_check_mark: | N/A                                   |
| `packageUrl`            | _string_                                                                                      | :heavy_minus_sign: | N/A                                   |
| `toolNames`             | _string_[]                                                                                    | :heavy_check_mark: | N/A                                   |
| `version`               | _string_                                                                                      | :heavy_check_mark: | The latest version of the integration |
| `versionCreatedAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                   |
| `versions`              | [components.IntegrationVersion](../../models/components/integrationversion.md)[]              | :heavy_minus_sign: | N/A                                   |
