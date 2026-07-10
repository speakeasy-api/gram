# RiskPolicyBypassRequest

## Example Usage

```typescript
import { RiskPolicyBypassRequest } from "@gram/client/models/components/riskpolicybypassrequest.js";

let value: RiskPolicyBypassRequest = {
  createdAt: new Date("2024-07-15T09:40:31.556Z"),
  grantedPrincipalUrns: [],
  id: "0751d321-8b8d-448b-8fd2-8efee7990d9b",
  policyId: "0dfae43f-4809-404c-ad04-01ed4046c1df",
  requesterUserId: "<id>",
  status: "denied",
  targetDimensions: {
    "key": "<value>",
    "key1": "<value>",
    "key2": "<value>",
  },
  updatedAt: new Date("2024-05-25T08:31:49.717Z"),
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)        | :heavy_check_mark:                                                                                   | Creation timestamp.                                                                                  |
| `decidedAt`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)        | :heavy_minus_sign:                                                                                   | Decision timestamp.                                                                                  |
| `decidedBy`                                                                                          | *string*                                                                                             | :heavy_minus_sign:                                                                                   | User ID that approved, denied, or revoked the request.                                               |
| `grantedPrincipalUrns`                                                                               | *string*[]                                                                                           | :heavy_check_mark:                                                                                   | Principal URNs granted when approved.                                                                |
| `id`                                                                                                 | *string*                                                                                             | :heavy_check_mark:                                                                                   | The bypass request ID.                                                                               |
| `note`                                                                                               | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Requester note.                                                                                      |
| `policyId`                                                                                           | *string*                                                                                             | :heavy_check_mark:                                                                                   | The risk policy ID.                                                                                  |
| `requesterEmail`                                                                                     | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Requester email when known.                                                                          |
| `requesterUserId`                                                                                    | *string*                                                                                             | :heavy_check_mark:                                                                                   | Requester user ID.                                                                                   |
| `status`                                                                                             | [components.RiskPolicyBypassRequestStatus](../../models/components/riskpolicybypassrequeststatus.md) | :heavy_check_mark:                                                                                   | Current request status.                                                                              |
| `targetDimensions`                                                                                   | Record<string, *string*>                                                                             | :heavy_check_mark:                                                                                   | Selector dimensions for the request target.                                                          |
| `targetKey`                                                                                          | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Canonical key for the target.                                                                        |
| `targetKind`                                                                                         | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Optional target namespace for the request, such as server_url.                                       |
| `targetLabel`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Optional display label for the target.                                                               |
| `updatedAt`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)        | :heavy_check_mark:                                                                                   | Last update timestamp.                                                                               |