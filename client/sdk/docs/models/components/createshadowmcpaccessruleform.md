# CreateShadowMCPAccessRuleForm

## Example Usage

```typescript
import { CreateShadowMCPAccessRuleForm } from "@gram/client/models/components/createshadowmcpaccessruleform.js";

let value: CreateShadowMCPAccessRuleForm = {
  accessScope: "organization",
  displayName: "Lennie_Howe",
  disposition: "denied",
  matchBreadth: "full_url",
  matchValue: "<value>",
};
```

## Fields

| Field                    | Type                                                                                                                         | Required           | Description                                                                                     |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------- |
| `accessScope`            | [components.CreateShadowMCPAccessRuleFormAccessScope](../../models/components/createshadowmcpaccessruleformaccessscope.md)   | :heavy_check_mark: | N/A                                                                                             |
| `displayName`            | _string_                                                                                                                     | :heavy_check_mark: | N/A                                                                                             |
| `disposition`            | [components.CreateShadowMCPAccessRuleFormDisposition](../../models/components/createshadowmcpaccessruleformdisposition.md)   | :heavy_check_mark: | N/A                                                                                             |
| `matchBreadth`           | [components.CreateShadowMCPAccessRuleFormMatchBreadth](../../models/components/createshadowmcpaccessruleformmatchbreadth.md) | :heavy_check_mark: | N/A                                                                                             |
| `matchValue`             | _string_                                                                                                                     | :heavy_check_mark: | N/A                                                                                             |
| `observedFullUrl`        | _string_                                                                                                                     | :heavy_minus_sign: | N/A                                                                                             |
| `observedServerIdentity` | _string_                                                                                                                     | :heavy_minus_sign: | N/A                                                                                             |
| `observedUrlHost`        | _string_                                                                                                                     | :heavy_minus_sign: | N/A                                                                                             |
| `projectId`              | _string_                                                                                                                     | :heavy_minus_sign: | N/A                                                                                             |
| `projectIds`             | _string_[]                                                                                                                   | :heavy_minus_sign: | Project ids to create project-scoped rules for. Empty uses project_id for single-rule creation. |
| `reason`                 | _string_                                                                                                                     | :heavy_minus_sign: | N/A                                                                                             |
