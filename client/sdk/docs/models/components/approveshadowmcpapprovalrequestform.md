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

| Field                    | Type                                                               | Required           | Description                                                                              |
| ------------------------ | ------------------------------------------------------------------ | ------------------ | ---------------------------------------------------------------------------------------- |
| `accessScope`            | [components.AccessScope](../../models/components/accessscope.md)   | :heavy_check_mark: | N/A                                                                                      |
| `displayName`            | _string_                                                           | :heavy_check_mark: | N/A                                                                                      |
| `id`                     | _string_                                                           | :heavy_check_mark: | N/A                                                                                      |
| `matchBreadth`           | [components.MatchBreadth](../../models/components/matchbreadth.md) | :heavy_check_mark: | N/A                                                                                      |
| `matchValue`             | _string_                                                           | :heavy_check_mark: | N/A                                                                                      |
| `observedFullUrl`        | _string_                                                           | :heavy_minus_sign: | N/A                                                                                      |
| `observedServerIdentity` | _string_                                                           | :heavy_minus_sign: | N/A                                                                                      |
| `observedUrlHost`        | _string_                                                           | :heavy_minus_sign: | N/A                                                                                      |
| `projectIds`             | _string_[]                                                         | :heavy_minus_sign: | Project ids to create project-scoped rules for. Empty falls back to the request project. |
| `reason`                 | _string_                                                           | :heavy_minus_sign: | N/A                                                                                      |
