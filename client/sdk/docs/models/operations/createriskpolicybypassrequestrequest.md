# CreateRiskPolicyBypassRequestRequest

## Example Usage

```typescript
import { CreateRiskPolicyBypassRequestRequest } from "@gram/client/models/operations/createriskpolicybypassrequest.js";

let value: CreateRiskPolicyBypassRequestRequest = {
  createShadowMCPApprovalRequestForm: {
    requestToken: "<value>",
  },
};
```

## Fields

| Field                                                                                                          | Type                                                                                                           | Required                                                                                                       | Description                                                                                                    |
| -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                                  | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | Session header                                                                                                 |
| `createShadowMCPApprovalRequestForm`                                                                           | [components.CreateShadowMCPApprovalRequestForm](../../models/components/createshadowmcpapprovalrequestform.md) | :heavy_check_mark:                                                                                             | N/A                                                                                                            |