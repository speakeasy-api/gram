# UpdateShadowMCPAccessRuleRequest

## Example Usage

```typescript
import { UpdateShadowMCPAccessRuleRequest } from "@gram/client/models/operations/updateshadowmcpaccessrule.js";

let value: UpdateShadowMCPAccessRuleRequest = {
  updateShadowMCPAccessRuleForm: {
    accessScope: "organization",
    displayName: "Willa_Lubowitz50",
    disposition: "allowed",
    id: "3717e04e-22a8-4e77-8fcb-cf8cdc14d7b6",
    matchBreadth: "full_url",
    matchValue: "<value>",
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `updateShadowMCPAccessRuleForm`                                                                      | [components.UpdateShadowMCPAccessRuleForm](../../models/components/updateshadowmcpaccessruleform.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |