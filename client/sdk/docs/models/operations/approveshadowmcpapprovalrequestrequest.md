# ApproveShadowMCPApprovalRequestRequest

## Example Usage

```typescript
import { ApproveShadowMCPApprovalRequestRequest } from "@gram/client/models/operations/approveshadowmcpapprovalrequest.js";

let value: ApproveShadowMCPApprovalRequestRequest = {
  approveShadowMCPApprovalRequestForm: {
    accessScope: "project",
    displayName: "Verna47",
    id: "9ae2bdd4-bc05-4002-92bb-7524ea45d760",
    matchBreadth: "full_url",
    matchValue: "<value>",
  },
};
```

## Fields

| Field                                 | Type                                                                                                             | Required           | Description    |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                         | _string_                                                                                                         | :heavy_minus_sign: | Session header |
| `approveShadowMCPApprovalRequestForm` | [components.ApproveShadowMCPApprovalRequestForm](../../models/components/approveshadowmcpapprovalrequestform.md) | :heavy_check_mark: | N/A            |
