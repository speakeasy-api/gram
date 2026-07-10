# ApproveRiskPolicyBypassRequestRequest

## Example Usage

```typescript
import { ApproveRiskPolicyBypassRequestRequest } from "@gram/client/models/operations/approveriskpolicybypassrequest.js";

let value: ApproveRiskPolicyBypassRequestRequest = {
  riskPolicyBypassApprovalRequestBody: {
    id: "815de180-f646-4e90-a940-32fff21f956c",
  },
};
```

## Fields

| Field                                                                                                            | Type                                                                                                             | Required                                                                                                         | Description                                                                                                      |
| ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                                        | *string*                                                                                                         | :heavy_minus_sign:                                                                                               | API Key header                                                                                                   |
| `gramSession`                                                                                                    | *string*                                                                                                         | :heavy_minus_sign:                                                                                               | Session header                                                                                                   |
| `gramProject`                                                                                                    | *string*                                                                                                         | :heavy_minus_sign:                                                                                               | project header                                                                                                   |
| `riskPolicyBypassApprovalRequestBody`                                                                            | [components.RiskPolicyBypassApprovalRequestBody](../../models/components/riskpolicybypassapprovalrequestbody.md) | :heavy_check_mark:                                                                                               | N/A                                                                                                              |