# RiskPolicyBypassApprovalRequestBody

## Example Usage

```typescript
import { RiskPolicyBypassApprovalRequestBody } from "@gram/client/models/components/riskpolicybypassapprovalrequestbody.js";

let value: RiskPolicyBypassApprovalRequestBody = {
  id: "e406c456-b8b4-4742-9fc0-6d524bd16ae6",
};
```

## Fields

| Field                  | Type       | Required           | Description                                                                                |
| ---------------------- | ---------- | ------------------ | ------------------------------------------------------------------------------------------ |
| `grantedPrincipalUrns` | _string_[] | :heavy_minus_sign: | Principal URNs to grant bypass access to. Use user:all for every user in the organization. |
| `id`                   | _string_   | :heavy_check_mark: | The bypass request ID.                                                                     |
