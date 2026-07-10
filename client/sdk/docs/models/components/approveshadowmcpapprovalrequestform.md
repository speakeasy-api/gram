# ApproveShadowMCPApprovalRequestForm

## Example Usage

```typescript
import { ApproveShadowMCPApprovalRequestForm } from "@gram/client/models/components/approveshadowmcpapprovalrequestform.js";

let value: ApproveShadowMCPApprovalRequestForm = {
  accessScope: "project",
  displayName: "Sunny_Greenfelder91",
  id: "08e3ac99-ef7d-4de8-a060-4a94228bab58",
  matchBreadth: "full_url",
  matchValue: "<value>",
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `accessScope`                                                                            | [components.AccessScope](../../models/components/accessscope.md)                         | :heavy_check_mark:                                                                       | N/A                                                                                      |
| `displayName`                                                                            | *string*                                                                                 | :heavy_check_mark:                                                                       | N/A                                                                                      |
| `id`                                                                                     | *string*                                                                                 | :heavy_check_mark:                                                                       | N/A                                                                                      |
| `matchBreadth`                                                                           | [components.MatchBreadth](../../models/components/matchbreadth.md)                       | :heavy_check_mark:                                                                       | N/A                                                                                      |
| `matchValue`                                                                             | *string*                                                                                 | :heavy_check_mark:                                                                       | N/A                                                                                      |
| `observedFullUrl`                                                                        | *string*                                                                                 | :heavy_minus_sign:                                                                       | N/A                                                                                      |
| `observedServerIdentity`                                                                 | *string*                                                                                 | :heavy_minus_sign:                                                                       | N/A                                                                                      |
| `observedUrlHost`                                                                        | *string*                                                                                 | :heavy_minus_sign:                                                                       | N/A                                                                                      |
| `projectIds`                                                                             | *string*[]                                                                               | :heavy_minus_sign:                                                                       | Project ids to create project-scoped rules for. Empty falls back to the request project. |
| `reason`                                                                                 | *string*                                                                                 | :heavy_minus_sign:                                                                       | N/A                                                                                      |