# CreateShadowMCPAccessRuleResult

## Example Usage

```typescript
import { CreateShadowMCPAccessRuleResult } from "@gram/client/models/components/createshadowmcpaccessruleresult.js";

let value: CreateShadowMCPAccessRuleResult = {
  rules: [
    {
      accessScope: "project",
      createdAt: new Date("2026-04-11T06:09:22.623Z"),
      displayName: "Dalton.Erdman",
      disposition: "denied",
      id: "a67c0f69-3051-43ee-9afc-7b0e6477dbeb",
      matchBreadth: "full_url",
      matchValue: "<value>",
      organizationId: "<id>",
      resourceType: "<value>",
      updatedAt: new Date("2026-12-12T13:08:12.512Z"),
    },
  ],
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `rules`                                                                            | [components.ShadowMCPAccessRule](../../models/components/shadowmcpaccessrule.md)[] | :heavy_check_mark:                                                                 | N/A                                                                                |