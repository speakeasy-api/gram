# CreateShadowMCPApprovalRequestRequest

## Example Usage

```typescript
import { CreateShadowMCPApprovalRequestRequest } from "@gram/client/models/operations/createshadowmcpapprovalrequest.js";

let value: CreateShadowMCPApprovalRequestRequest = {
  createShadowMCPApprovalRequestForm: {
    requestToken: "<value>",
  },
};
```

## Fields

| Field                                | Type                                                                                                           | Required           | Description    |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                        | _string_                                                                                                       | :heavy_minus_sign: | Session header |
| `createShadowMCPApprovalRequestForm` | [components.CreateShadowMCPApprovalRequestForm](../../models/components/createshadowmcpapprovalrequestform.md) | :heavy_check_mark: | N/A            |
