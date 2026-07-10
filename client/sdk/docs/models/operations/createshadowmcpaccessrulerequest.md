# CreateShadowMCPAccessRuleRequest

## Example Usage

```typescript
import { CreateShadowMCPAccessRuleRequest } from "@gram/client/models/operations/createshadowmcpaccessrule.js";

let value: CreateShadowMCPAccessRuleRequest = {
  createShadowMCPAccessRuleForm: {
    accessScope: "organization",
    displayName: "Frankie_Tromp",
    disposition: "allowed",
    matchBreadth: "url_host",
    matchValue: "<value>",
  },
};
```

## Fields

| Field                           | Type                                                                                                 | Required           | Description    |
| ------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                   | _string_                                                                                             | :heavy_minus_sign: | Session header |
| `createShadowMCPAccessRuleForm` | [components.CreateShadowMCPAccessRuleForm](../../models/components/createshadowmcpaccessruleform.md) | :heavy_check_mark: | N/A            |
