# ShadowMCPAccessRule

## Example Usage

```typescript
import { ShadowMCPAccessRule } from "@gram/client/models/components/shadowmcpaccessrule.js";

let value: ShadowMCPAccessRule = {
  accessScope: "organization",
  createdAt: new Date("2026-12-07T00:39:17.358Z"),
  displayName: "Dagmar99",
  disposition: "allowed",
  id: "8d26e16c-733d-470b-838f-5474962fe668",
  matchBreadth: "full_url",
  matchValue: "<value>",
  organizationId: "<id>",
  resourceType: "<value>",
  updatedAt: new Date("2026-01-04T17:40:39.779Z"),
};
```

## Fields

| Field                    | Type                                                                                                     | Required           | Description |
| ------------------------ | -------------------------------------------------------------------------------------------------------- | ------------------ | ----------- |
| `accessScope`            | [components.ShadowMCPAccessRuleAccessScope](../../models/components/shadowmcpaccessruleaccessscope.md)   | :heavy_check_mark: | N/A         |
| `createdAt`              | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)            | :heavy_check_mark: | N/A         |
| `createdBy`              | _string_                                                                                                 | :heavy_minus_sign: | N/A         |
| `displayName`            | _string_                                                                                                 | :heavy_check_mark: | N/A         |
| `disposition`            | [components.Disposition](../../models/components/disposition.md)                                         | :heavy_check_mark: | N/A         |
| `id`                     | _string_                                                                                                 | :heavy_check_mark: | N/A         |
| `matchBreadth`           | [components.ShadowMCPAccessRuleMatchBreadth](../../models/components/shadowmcpaccessrulematchbreadth.md) | :heavy_check_mark: | N/A         |
| `matchValue`             | _string_                                                                                                 | :heavy_check_mark: | N/A         |
| `observedFullUrl`        | _string_                                                                                                 | :heavy_minus_sign: | N/A         |
| `observedServerIdentity` | _string_                                                                                                 | :heavy_minus_sign: | N/A         |
| `observedUrlHost`        | _string_                                                                                                 | :heavy_minus_sign: | N/A         |
| `organizationId`         | _string_                                                                                                 | :heavy_check_mark: | N/A         |
| `projectId`              | _string_                                                                                                 | :heavy_minus_sign: | N/A         |
| `reason`                 | _string_                                                                                                 | :heavy_minus_sign: | N/A         |
| `resourceType`           | _string_                                                                                                 | :heavy_check_mark: | N/A         |
| `sourceRequestId`        | _string_                                                                                                 | :heavy_minus_sign: | N/A         |
| `updatedAt`              | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)            | :heavy_check_mark: | N/A         |
| `updatedBy`              | _string_                                                                                                 | :heavy_minus_sign: | N/A         |
