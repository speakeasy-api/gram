# ShadowMCPApprovalDecisionResult

## Example Usage

```typescript
import { ShadowMCPApprovalDecisionResult } from "@gram/client/models/components/shadowmcpapprovaldecisionresult.js";

let value: ShadowMCPApprovalDecisionResult = {
  request: {
    blockedCount: 240985,
    createdAt: new Date("2024-01-04T04:32:23.728Z"),
    id: "674edcad-24b1-4fed-835b-011a7d92ef0d",
    organizationId: "<id>",
    projectId: "d6514282-8790-46a4-8a44-dc158cf8dcca",
    requestedAt: new Date("2026-10-26T20:03:00.845Z"),
    resourceType: "<value>",
    status: "requested",
    updatedAt: new Date("2026-10-05T01:54:19.164Z"),
  },
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

| Field     | Type                                                                                       | Required           | Description |
| --------- | ------------------------------------------------------------------------------------------ | ------------------ | ----------- |
| `request` | [components.ShadowMCPApprovalRequest](../../models/components/shadowmcpapprovalrequest.md) | :heavy_check_mark: | N/A         |
| `rule`    | [components.ShadowMCPAccessRule](../../models/components/shadowmcpaccessrule.md)           | :heavy_minus_sign: | N/A         |
| `rules`   | [components.ShadowMCPAccessRule](../../models/components/shadowmcpaccessrule.md)[]         | :heavy_check_mark: | N/A         |
