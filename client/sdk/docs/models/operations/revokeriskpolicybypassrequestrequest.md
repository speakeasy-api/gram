# RevokeRiskPolicyBypassRequestRequest

## Example Usage

```typescript
import { RevokeRiskPolicyBypassRequestRequest } from "@gram/client/models/operations/revokeriskpolicybypassrequest.js";

let value: RevokeRiskPolicyBypassRequestRequest = {
  riskIDRequestBody: {
    id: "cf7a8865-606f-49e7-ac07-b45f68f2fc8f",
  },
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `gramKey`                                                                    | *string*                                                                     | :heavy_minus_sign:                                                           | API Key header                                                               |
| `gramSession`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | Session header                                                               |
| `gramProject`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | project header                                                               |
| `riskIDRequestBody`                                                          | [components.RiskIDRequestBody](../../models/components/riskidrequestbody.md) | :heavy_check_mark:                                                           | N/A                                                                          |