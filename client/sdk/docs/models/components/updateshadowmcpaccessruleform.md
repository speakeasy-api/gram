# UpdateShadowMCPAccessRuleForm

## Example Usage

```typescript
import { UpdateShadowMCPAccessRuleForm } from "@gram/client/models/components/updateshadowmcpaccessruleform.js";

let value: UpdateShadowMCPAccessRuleForm = {
  accessScope: "project",
  displayName: "Blaze.Wintheiser",
  disposition: "denied",
  id: "a3a28cbd-1be8-4db4-b781-1956df3a1e8f",
  matchBreadth: "url_host",
  matchValue: "<value>",
};
```

## Fields

| Field                    | Type                                                                                                                         | Required           | Description |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------- | ------------------ | ----------- |
| `accessScope`            | [components.UpdateShadowMCPAccessRuleFormAccessScope](../../models/components/updateshadowmcpaccessruleformaccessscope.md)   | :heavy_check_mark: | N/A         |
| `displayName`            | _string_                                                                                                                     | :heavy_check_mark: | N/A         |
| `disposition`            | [components.UpdateShadowMCPAccessRuleFormDisposition](../../models/components/updateshadowmcpaccessruleformdisposition.md)   | :heavy_check_mark: | N/A         |
| `id`                     | _string_                                                                                                                     | :heavy_check_mark: | N/A         |
| `matchBreadth`           | [components.UpdateShadowMCPAccessRuleFormMatchBreadth](../../models/components/updateshadowmcpaccessruleformmatchbreadth.md) | :heavy_check_mark: | N/A         |
| `matchValue`             | _string_                                                                                                                     | :heavy_check_mark: | N/A         |
| `observedFullUrl`        | _string_                                                                                                                     | :heavy_minus_sign: | N/A         |
| `observedServerIdentity` | _string_                                                                                                                     | :heavy_minus_sign: | N/A         |
| `observedUrlHost`        | _string_                                                                                                                     | :heavy_minus_sign: | N/A         |
| `projectId`              | _string_                                                                                                                     | :heavy_minus_sign: | N/A         |
| `reason`                 | _string_                                                                                                                     | :heavy_minus_sign: | N/A         |
