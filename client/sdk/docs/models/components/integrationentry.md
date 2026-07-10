# IntegrationEntry

## Example Usage

```typescript
import { IntegrationEntry } from "@gram/client/models/components/integrationentry.js";

let value: IntegrationEntry = {
  packageId: "<id>",
  packageName: "<value>",
  toolNames: [],
  version: "<value>",
  versionCreatedAt: new Date("2026-11-21T22:12:24.981Z"),
};
```

## Fields

| Field                 | Type                                                                                          | Required           | Description |
| --------------------- | --------------------------------------------------------------------------------------------- | ------------------ | ----------- |
| `packageId`           | _string_                                                                                      | :heavy_check_mark: | N/A         |
| `packageImageAssetId` | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `packageKeywords`     | _string_[]                                                                                    | :heavy_minus_sign: | N/A         |
| `packageName`         | _string_                                                                                      | :heavy_check_mark: | N/A         |
| `packageSummary`      | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `packageTitle`        | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `packageUrl`          | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `toolNames`           | _string_[]                                                                                    | :heavy_check_mark: | N/A         |
| `version`             | _string_                                                                                      | :heavy_check_mark: | N/A         |
| `versionCreatedAt`    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A         |
